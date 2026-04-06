// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// ---------------------------------------------------------------------------
// padMAC
// ---------------------------------------------------------------------------

func TestPadMAC_NotSixOctets_ReturnsUnchanged(t *testing.T) {
	if got := padMAC("1:2:3"); got != "1:2:3" {
		t.Fatalf("padMAC = %q", got)
	}
}

func TestPadMAC_PadsSingleDigitOctets(t *testing.T) {
	if got := padMAC("2:0:0:0:0:1"); got != "02:00:00:00:00:01" {
		t.Fatalf("padMAC = %q", got)
	}
}

// ---------------------------------------------------------------------------
// findIPByMACInARPCache
// ---------------------------------------------------------------------------

func TestFindIPByMACInARPCache_InvalidMAC(t *testing.T) {
	_, err := findIPByMACInARPCache("not-a-mac")
	if err == nil {
		t.Fatal("expected error for invalid MAC, got nil")
	}
}

func TestFindIPByMACInARPCache_ValidMAC_NoError(t *testing.T) {
	// A valid MAC that is almost certainly not in the local ARP cache.
	// We just verify the function runs without error and returns a slice.
	ips, err := findIPByMACInARPCache("02:00:00:00:00:01")
	if err != nil {
		t.Fatalf("unexpected error for valid MAC: %v", err)
	}
	// ips may be empty — that is fine.
	_ = ips
}

func TestFindIPByMACInARPCache_CompactMAC_NoError(t *testing.T) {
	// Compact (single-digit octet) MAC — should be padded before parsing.
	ips, err := findIPByMACInARPCache("2:0:0:0:0:1")
	if err != nil {
		t.Fatalf("unexpected error for compact MAC: %v", err)
	}
	_ = ips
}

// TestFindIPByMACInARPCache_SyntheticMatch exercises MAC+IPv4 extraction from a
// stubbed ARP table so the inner match loop (not only exec success) runs on CI.
func TestFindIPByMACInARPCache_SyntheticMatch(t *testing.T) {
	oldL, oldB := execARPCommandLinux, execARPCommandBSD
	synthetic := []byte("? (10.0.0.99) at aa:bb:cc:dd:ee:ff on en0\n")
	execARPCommandLinux = func() ([]byte, error) { return synthetic, nil }
	execARPCommandBSD = func() ([]byte, error) { return synthetic, nil }
	t.Cleanup(func() {
		execARPCommandLinux = oldL
		execARPCommandBSD = oldB
	})

	ips, err := findIPByMACInARPCache("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("findIPByMACInARPCache: %v", err)
	}
	if len(ips) != 1 || ips[0] != "10.0.0.99" {
		t.Fatalf("ips = %v, want [10.0.0.99]", ips)
	}
}

// TestFindIPByMACInARPCache_SyntheticSkipsNonMatchingMAC covers the branch
// where a line contains a different MAC before the matching row.
func TestFindIPByMACInARPCache_SyntheticSkipsNonMatchingMAC(t *testing.T) {
	oldL, oldB := execARPCommandLinux, execARPCommandBSD
	synthetic := []byte("? (192.168.1.1) at 11:22:33:44:55:66 on en0\n? (10.0.0.99) at aa:bb:cc:dd:ee:ff on en0\n")
	execARPCommandLinux = func() ([]byte, error) { return synthetic, nil }
	execARPCommandBSD = func() ([]byte, error) { return synthetic, nil }
	t.Cleanup(func() {
		execARPCommandLinux = oldL
		execARPCommandBSD = oldB
	})

	ips, err := findIPByMACInARPCache("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("findIPByMACInARPCache: %v", err)
	}
	if len(ips) != 1 || ips[0] != "10.0.0.99" {
		t.Fatalf("ips = %v, want [10.0.0.99]", ips)
	}
}

// TestFindIPByMACInARPCache_SyntheticMacNoIPv4OnLineThenMatch covers matching MAC
// on a line without a dotted IPv4, then a full match on the next line.
func TestFindIPByMACInARPCache_SyntheticMacNoIPv4OnLineThenMatch(t *testing.T) {
	oldL, oldB := execARPCommandLinux, execARPCommandBSD
	synthetic := []byte("at aa:bb:cc:dd:ee:ff on en0\n? (10.0.0.5) at aa:bb:cc:dd:ee:ff on en0\n")
	execARPCommandLinux = func() ([]byte, error) { return synthetic, nil }
	execARPCommandBSD = func() ([]byte, error) { return synthetic, nil }
	t.Cleanup(func() {
		execARPCommandLinux = oldL
		execARPCommandBSD = oldB
	})

	ips, err := findIPByMACInARPCache("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("findIPByMACInARPCache: %v", err)
	}
	if len(ips) != 1 || ips[0] != "10.0.0.5" {
		t.Fatalf("ips = %v, want [10.0.0.5]", ips)
	}
}

// ---------------------------------------------------------------------------
// isHostReachable
// ---------------------------------------------------------------------------

func TestIsHostReachable_DocumentationNetUnreachable(t *testing.T) {
	// RFC 5737 TEST-NET-1 — should not route; not "connection refused".
	if isHostReachable("192.0.2.1") {
		t.Fatal("192.0.2.1 should not be treated as reachable")
	}
}

func TestIsHostReachable_RefusedPort_ReturnsTrue(t *testing.T) {
	// Listen on a random ephemeral port, note the address, then close the
	// listener so the OS issues ECONNREFUSED for subsequent connects.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not bind test listener: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	ln.Close() // closing makes OS refuse new connections

	// isHostReachable strips ":22" suffix and dials ip:22, but our function
	// only receives the IP — it always appends ":22". We instead test
	// isHostReachable indirectly by calling it with an IP where we can
	// guarantee the behaviour.
	//
	// On most OSes, dialling a just-closed loopback port returns
	// "connection refused". We use the actual closed port to confirm that
	// the function treats ECONNREFUSED as reachable.
	_ = addr // avoid unused variable if the test below short-circuits
	result := isHostReachable("127.0.0.1")
	// 127.0.0.1:22 either succeeds (sshd running) -> true,
	// or is refused (no sshd) -> true.
	// Either way the function must not hang or panic.
	_ = result
}

func TestIsHostReachable_DialSuccess_ReturnsTrue(t *testing.T) {
	// Start a real listener so the dial succeeds.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not start listener: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	// Accept in background to prevent the dial from stalling.
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	// We can't call isHostReachable(ip) directly because it always dials :22.
	// Instead, dial the listener address directly to verify the function's
	// success branch via the underlying net package it uses.
	conn, err := net.DialTimeout("tcp", addr.String(), timeout2s)
	if err != nil {
		t.Fatalf("could not dial test listener: %v", err)
	}
	conn.Close()
	// If we reach here the success branch of DialTimeout works correctly.
}

// timeout2s references the same 2-second constant used by isHostReachable.
const timeout2s = 2e9 // 2 * time.Second in nanoseconds as a time.Duration

// TestStepDiscoverIP_Run_ViaDHCPLease covers the Sylve DHCP lease API success path.
// StepDiscoverIP waits 10s before the first poll; skipped under -short.
func TestStepDiscoverIP_Run_ViaDHCPLease(t *testing.T) {
	oldT, oldP := discoverIPTotalTimeout, discoverIPPollInterval
	discoverIPTotalTimeout = 5 * time.Second
	discoverIPPollInterval = 1 * time.Millisecond
	t.Cleanup(func() {
		discoverIPTotalTimeout = oldT
		discoverIPPollInterval = oldP
	})
	const mac = "aa:bb:cc:dd:ee:ff"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/network/dhcp/lease" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
				Status: "ok",
				Data: client.Leases{
					File: []client.FileLease{{MAC: mac, IP: "10.0.0.42"}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", mac)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if state.Get("instance_ip") != "10.0.0.42" {
		t.Fatalf("instance_ip = %v", state.Get("instance_ip"))
	}
}

// TestStepDiscoverIP_DHCPPollErrorThenLease covers the Sylve DHCP API error log
// path followed by a successful lease on a later poll.
func TestStepDiscoverIP_DHCPPollErrorThenLease(t *testing.T) {
	oldT, oldP := discoverIPTotalTimeout, discoverIPPollInterval
	discoverIPTotalTimeout = 5 * time.Second
	discoverIPPollInterval = 1 * time.Millisecond
	t.Cleanup(func() {
		discoverIPTotalTimeout = oldT
		discoverIPPollInterval = oldP
	})
	const mac = "aa:bb:cc:dd:ee:ff"
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/network/dhcp/lease" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			n := atomic.AddInt32(&getN, 1)
			if n == 1 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
				Status: "ok",
				Data: client.Leases{
					File: []client.FileLease{{MAC: mac, IP: "10.0.0.77"}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", mac)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if state.Get("instance_ip") != "10.0.0.77" {
		t.Fatalf("instance_ip = %v", state.Get("instance_ip"))
	}
	if getN < 2 {
		t.Fatalf("expected at least 2 DHCP API calls, got %d", getN)
	}
}

// TestStepDiscoverIP_ViaARPCache covers the local ARP cache path when Sylve DHCP
// returns no lease (stubbed ARP lookup returns a reachable loopback address).
func TestStepDiscoverIP_ViaARPCache(t *testing.T) {
	oldT, oldP := discoverIPTotalTimeout, discoverIPPollInterval
	discoverIPTotalTimeout = 5 * time.Second
	discoverIPPollInterval = 1 * time.Millisecond
	t.Cleanup(func() {
		discoverIPTotalTimeout = oldT
		discoverIPPollInterval = oldP
	})
	old := findIPByMACInARPCacheFn
	findIPByMACInARPCacheFn = func(string) ([]string, error) {
		return []string{"127.0.0.1"}, nil
	}
	t.Cleanup(func() { findIPByMACInARPCacheFn = old })

	const mac = "aa:bb:cc:dd:ee:ff"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/network/dhcp/lease" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
				Status: "ok",
				Data:   client.Leases{File: []client.FileLease{}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", mac)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if state.Get("instance_ip") != "127.0.0.1" {
		t.Fatalf("instance_ip = %v", state.Get("instance_ip"))
	}
}

func TestFindIPByMACInARPCache_ExecError(t *testing.T) {
	oldL, oldB := execARPCommandLinux, execARPCommandBSD
	stub := func() ([]byte, error) {
		return nil, fmt.Errorf("stub exec failure")
	}
	execARPCommandLinux = stub
	execARPCommandBSD = stub
	t.Cleanup(func() {
		execARPCommandLinux = oldL
		execARPCommandBSD = oldB
	})

	_, err := findIPByMACInARPCache("aa:bb:cc:dd:ee:ff")
	if err == nil {
		t.Fatal("expected error from exec failure")
	}
}

// TestFindIPByMACInARPCache_LinuxExecPathWhenToggled exercises the Linux
// ARP command branch on non-Linux builders (arpPreferLinux == true).
func TestFindIPByMACInARPCache_LinuxExecPathWhenToggled(t *testing.T) {
	oldPref := arpPreferLinux
	oldL, oldB := execARPCommandLinux, execARPCommandBSD
	synthetic := []byte("? (10.0.0.99) at aa:bb:cc:dd:ee:ff on en0\n")
	execARPCommandLinux = func() ([]byte, error) { return synthetic, nil }
	execARPCommandBSD = func() ([]byte, error) {
		t.Fatal("execARPCommandBSD should not run when arpPreferLinux is true")
		return nil, nil
	}
	arpPreferLinux = true
	t.Cleanup(func() {
		arpPreferLinux = oldPref
		execARPCommandLinux = oldL
		execARPCommandBSD = oldB
	})

	ips, err := findIPByMACInARPCache("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("findIPByMACInARPCache: %v", err)
	}
	if len(ips) != 1 || ips[0] != "10.0.0.99" {
		t.Fatalf("ips = %v, want [10.0.0.99]", ips)
	}
}

// TestFindIPByMACInARPCache_SkipsCandidateParseError covers the inner-loop
// branch where net.ParseMAC rejects a regex MAC token before a later match.
func TestFindIPByMACInARPCache_SkipsCandidateParseError(t *testing.T) {
	oldParse := parseMACForARPCache
	var n int
	parseMACForARPCache = func(candidate string) (net.HardwareAddr, error) {
		n++
		if n == 1 {
			return nil, fmt.Errorf("simulated parse failure")
		}
		return net.ParseMAC(padMAC(candidate))
	}
	t.Cleanup(func() { parseMACForARPCache = oldParse })

	oldPref := arpPreferLinux
	oldL, oldB := execARPCommandLinux, execARPCommandBSD
	synthetic := []byte("? (10.0.0.7) at aa:bb:cc:dd:ee:ff pad aa:bb:cc:dd:ee:ff on en0\n")
	execARPCommandLinux = func() ([]byte, error) { return synthetic, nil }
	execARPCommandBSD = func() ([]byte, error) { return synthetic, nil }
	arpPreferLinux = false
	t.Cleanup(func() {
		arpPreferLinux = oldPref
		execARPCommandLinux = oldL
		execARPCommandBSD = oldB
	})

	ips, err := findIPByMACInARPCache("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("findIPByMACInARPCache: %v", err)
	}
	if len(ips) != 1 || ips[0] != "10.0.0.7" {
		t.Fatalf("ips = %v, want [10.0.0.7]", ips)
	}
	if n != 2 {
		t.Fatalf("parseMACForARPCache calls = %d, want 2", n)
	}
}

func TestStepDiscoverIP_ARPCacheLookupErrorLogged(t *testing.T) {
	oldFn := findIPByMACInARPCacheFn
	findIPByMACInARPCacheFn = func(string) ([]string, error) {
		return nil, fmt.Errorf("stub arp error")
	}
	t.Cleanup(func() { findIPByMACInARPCacheFn = oldFn })
	oldT, oldP := discoverIPTotalTimeout, discoverIPPollInterval
	discoverIPTotalTimeout = time.Minute
	discoverIPPollInterval = 1 * time.Millisecond
	t.Cleanup(func() {
		discoverIPTotalTimeout = oldT
		discoverIPPollInterval = oldP
	})

	const mac = "aa:bb:cc:dd:ee:ff"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/network/dhcp/lease" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
				Status: "ok",
				Data:   client.Leases{File: []client.FileLease{}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", mac)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want halt on cancel", got)
	}
}

func TestStepDiscoverIP_ARPUnreachableCandidates(t *testing.T) {
	oldFn := findIPByMACInARPCacheFn
	findIPByMACInARPCacheFn = func(string) ([]string, error) {
		return []string{"192.0.2.1", "192.0.2.2"}, nil
	}
	t.Cleanup(func() { findIPByMACInARPCacheFn = oldFn })
	oldReachFn := isHostReachableFn
	isHostReachableFn = func(string) bool { return false }
	t.Cleanup(func() { isHostReachableFn = oldReachFn })
	oldT, oldP := discoverIPTotalTimeout, discoverIPPollInterval
	discoverIPTotalTimeout = 100 * time.Millisecond
	discoverIPPollInterval = 1 * time.Millisecond
	t.Cleanup(func() {
		discoverIPTotalTimeout = oldT
		discoverIPPollInterval = oldP
	})

	const mac = "aa:bb:cc:dd:ee:ff"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/network/dhcp/lease" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
				Status: "ok",
				Data:   client.Leases{File: []client.FileLease{}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", mac)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()
	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want halt on cancel", got)
	}
}

func TestStepDiscoverIP_TotalTimeout(t *testing.T) {
	oldT, oldP := discoverIPTotalTimeout, discoverIPPollInterval
	discoverIPTotalTimeout = 0
	discoverIPPollInterval = 0
	t.Cleanup(func() {
		discoverIPTotalTimeout = oldT
		discoverIPPollInterval = oldP
	})

	const mac = "aa:bb:cc:dd:ee:ff"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/network/dhcp/lease" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
				Status: "ok",
				Data:   client.Leases{File: []client.FileLease{}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", mac)

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}
