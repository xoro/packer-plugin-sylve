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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func testHTTPServerListVMs(t *testing.T, vms []client.SimpleVM) (*client.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/vm/simple" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{
			Status: "ok",
			Data:   vms,
		})
	}))
	c := client.New(srv.URL, "tok", false)
	c.BaseURL = srv.URL
	return c, srv
}

func TestSelectVNCPort_Success(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	api, srv := testHTTPServerListVMs(t, nil)
	defer srv.Close()

	step := &StepCreateVM{Config: &Config{
		VNCPortMin: port,
		VNCPortMax: port,
		VNCHost:    "127.0.0.1",
	}}
	state := new(multistep.BasicStateBag)
	if err := step.selectVNCPort(api, state); err != nil {
		t.Fatalf("selectVNCPort: %v", err)
	}
	if step.Config.VNCPort != port {
		t.Fatalf("VNCPort = %d, want %d", step.Config.VNCPort, port)
	}
	raw := state.Get("vnc_view_listener")
	if raw == nil {
		t.Fatal("expected vnc_view_listener in state")
	}
	_ = raw.(net.Listener).Close()
}

func TestSelectVNCPort_SkipsPortClaimedByVM(t *testing.T) {
	// Find two consecutive TCP ports that are both free right now.
	var pLow, pHigh int
	found := false
	for base := 40000; base < 40200; base++ {
		l1, err1 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base))
		l2, err2 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base+1))
		if err1 == nil && err2 == nil {
			pLow, pHigh = base, base+1
			_ = l1.Close()
			_ = l2.Close()
			found = true
			break
		}
		if l1 != nil {
			_ = l1.Close()
		}
		if l2 != nil {
			_ = l2.Close()
		}
	}
	if !found {
		t.Skip("could not find two consecutive free ports in scan range")
	}

	vms := []client.SimpleVM{{RID: 1, Name: "a", VNCPort: pLow}}
	api, srv := testHTTPServerListVMs(t, vms)
	defer srv.Close()

	step := &StepCreateVM{Config: &Config{
		VNCPortMin: pLow,
		VNCPortMax: pHigh,
		VNCHost:    "127.0.0.1",
	}}
	state := new(multistep.BasicStateBag)
	if err := step.selectVNCPort(api, state); err != nil {
		t.Fatalf("selectVNCPort: %v", err)
	}
	if step.Config.VNCPort != pHigh {
		t.Fatalf("VNCPort = %d, want %d (skipped VM port %d)", step.Config.VNCPort, pHigh, pLow)
	}
	_ = state.Get("vnc_view_listener").(net.Listener).Close()
}

func TestSelectVNCPort_ListAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	api := client.New(srv.URL, "tok", false)
	api.BaseURL = srv.URL

	step := &StepCreateVM{Config: &Config{
		VNCPortMin: 5900,
		VNCPortMax: 5900,
		VNCHost:    "127.0.0.1",
	}}
	state := new(multistep.BasicStateBag)
	if err := step.selectVNCPort(api, state); err == nil {
		t.Fatal("expected error from ListVMsSimple")
	}
}

// TestSelectVNCPort_RemoteProbeSkipsWhenTCPResponds exercises the branch where
// VNCHost is treated as remote and a TCP probe succeeds, so the port is skipped.
func TestSelectVNCPort_RemoteProbeSkipsWhenTCPResponds(t *testing.T) {
	var pLow, pHigh int
	found := false
	for base := 41000; base < 41200; base++ {
		l1, err1 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base))
		l2, err2 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base+1))
		if err1 == nil && err2 == nil {
			pLow, pHigh = base, base+1
			_ = l1.Close()
			_ = l2.Close()
			found = true
			break
		}
		if l1 != nil {
			_ = l1.Close()
		}
		if l2 != nil {
			_ = l2.Close()
		}
	}
	if !found {
		t.Skip("could not find two consecutive free ports in scan range")
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", pLow))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	oldHost := isRemoteHostForVNCPort
	oldDial := dialTCPForVNCPortProbe
	isRemoteHostForVNCPort = func(string) bool { return true }
	dialTCPForVNCPortProbe = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(network, addr, timeout)
	}
	t.Cleanup(func() {
		isRemoteHostForVNCPort = oldHost
		dialTCPForVNCPortProbe = oldDial
	})

	api, srv := testHTTPServerListVMs(t, nil)
	defer srv.Close()

	step := &StepCreateVM{Config: &Config{
		VNCPortMin: pLow,
		VNCPortMax: pHigh,
		VNCHost:    "127.0.0.1",
	}}
	state := new(multistep.BasicStateBag)
	if err := step.selectVNCPort(api, state); err != nil {
		t.Fatalf("selectVNCPort: %v", err)
	}
	if step.Config.VNCPort != pHigh {
		t.Fatalf("VNCPort = %d, want %d (remote probe should skip %d)", step.Config.VNCPort, pHigh, pLow)
	}
	_ = state.Get("vnc_view_listener").(net.Listener).Close()
}

func TestSelectVNCPort_NoFreePortInRange(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	// Keep listener open so selectVNCPort cannot bind the same port.

	api, srv := testHTTPServerListVMs(t, nil)
	defer srv.Close()
	defer ln.Close()

	step := &StepCreateVM{Config: &Config{
		VNCPortMin: port,
		VNCPortMax: port,
		VNCHost:    "127.0.0.1",
	}}
	state := new(multistep.BasicStateBag)
	if err := step.selectVNCPort(api, state); err == nil {
		t.Fatal("expected error when port range is exhausted")
	}
}

// TestSelectVNCPort_SkipsTimeWaitPort verifies that selectVNCPort skips a port
// when the exclusive bind check fails (simulating a TIME_WAIT socket that
// Go's SO_REUSEADDR-based net.Listen would silently accept but bhyve would not).
func TestSelectVNCPort_SkipsTimeWaitPort(t *testing.T) {
	var pLow, pHigh int
	found := false
	for base := 42000; base < 42200; base++ {
		l1, err1 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base))
		l2, err2 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base+1))
		if err1 == nil && err2 == nil {
			pLow, pHigh = base, base+1
			_ = l1.Close()
			_ = l2.Close()
			found = true
			break
		}
		if l1 != nil {
			_ = l1.Close()
		}
		if l2 != nil {
			_ = l2.Close()
		}
	}
	if !found {
		t.Skip("could not find two consecutive free ports in scan range")
	}

	// Simulate a TIME_WAIT socket on pLow: the exclusive listen check fails for
	// pLow but the regular net.Listen (with SO_REUSEADDR) would succeed.
	oldExclusive := exclusiveListenFn
	exclusiveListenFn = func(addr string) error {
		if addr == fmt.Sprintf("127.0.0.1:%d", pLow) {
			return fmt.Errorf("simulated TIME_WAIT on %s", addr)
		}
		return oldExclusive(addr)
	}
	t.Cleanup(func() { exclusiveListenFn = oldExclusive })

	api, srv := testHTTPServerListVMs(t, nil)
	defer srv.Close()

	step := &StepCreateVM{Config: &Config{
		VNCPortMin: pLow,
		VNCPortMax: pHigh,
		VNCHost:    "127.0.0.1",
	}}
	state := new(multistep.BasicStateBag)
	if err := step.selectVNCPort(api, state); err != nil {
		t.Fatalf("selectVNCPort: %v", err)
	}
	if step.Config.VNCPort != pHigh {
		t.Fatalf("VNCPort = %d, want %d (should skip TIME_WAIT port %d)", step.Config.VNCPort, pHigh, pLow)
	}
	_ = state.Get("vnc_view_listener").(net.Listener).Close()
}

func TestStepCreateVM_Cleanup_SkipsWhenStepVMRIDZero(t *testing.T) {
	var del int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&del, 1)
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepCreateVM{
		Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true},
		vmRID:  0,
		ctx:    context.Background(),
	}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(42))

	step.Cleanup(state)
	if del != 0 {
		t.Fatalf("expected no DeleteVM when VM RID is 0, got %d calls", del)
	}
}

func TestStepCreateVM_Cleanup_SkipsWhenStateVMRIDZero(t *testing.T) {
	var del int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&del, 1)
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepCreateVM{
		Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true},
		vmRID:  99,
		ctx:    context.Background(),
	}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(0))

	step.Cleanup(state)
	if del != 0 {
		t.Fatalf("expected no DeleteVM when state vm_rid is 0, got %d calls", del)
	}
}

func TestStepCreateVM_Cleanup_KeepOnError_SkipsDelete(t *testing.T) {
	var delCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/vm/") {
			atomic.AddInt32(&delCalls, 1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepCreateVM{
		Config: &Config{
			SylveURL:      srv.URL,
			SylveToken:    "tok",
			TLSSkipVerify: true,
			KeepOnError:   true,
		},
		vmRID: 42,
		ctx:   context.Background(),
	}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(42))
	state.Put(multistep.StateHalted, true)

	step.Cleanup(state)
	if delCalls != 0 {
		t.Fatalf("DeleteVM called %d times, want 0", delCalls)
	}
}

func TestStepCreateVM_Cleanup_DestroyFalse_SuccessSkipsDelete(t *testing.T) {
	var delCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/vm/") {
			atomic.AddInt32(&delCalls, 1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepCreateVM{
		Config: &Config{
			SylveURL:      srv.URL,
			SylveToken:    "tok",
			TLSSkipVerify: true,
			Destroy:       false,
		},
		vmRID: 9,
		ctx:   context.Background(),
	}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))
	// Successful build: neither halted nor cancelled — destroy=false must keep VM.

	step.Cleanup(state)
	if delCalls != 0 {
		t.Fatalf("DeleteVM called %d times, want 0 when destroy=false on success", delCalls)
	}
}

func TestStepCreateVM_Cleanup_DeleteErrorStillRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/vm/") {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepCreateVM{
		Config: &Config{
			SylveURL:      srv.URL,
			SylveToken:    "tok",
			TLSSkipVerify: true,
		},
		vmRID: 99,
		ctx:   context.Background(),
	}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(99))
	// Failed build: Cleanup must still attempt deletion when destroy=false.
	state.Put(multistep.StateHalted, true)

	step.Cleanup(state)
}
