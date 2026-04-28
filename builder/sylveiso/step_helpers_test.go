// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"testing"
)

// ---------------------------------------------------------------------------
// boolPtr
// ---------------------------------------------------------------------------

func TestBoolPtr_True(t *testing.T) {
	b := boolPtr(true)
	if b == nil {
		t.Fatal("boolPtr(true) returned nil")
	}
	if !*b {
		t.Error("*boolPtr(true) = false, want true")
	}
}

func TestBoolPtr_False(t *testing.T) {
	b := boolPtr(false)
	if b == nil {
		t.Fatal("boolPtr(false) returned nil")
	}
	if *b {
		t.Error("*boolPtr(false) = true, want false")
	}
}

func TestBoolPtr_ReturnsDistinctPointers(t *testing.T) {
	a := boolPtr(true)
	b := boolPtr(true)
	if a == b {
		t.Error("boolPtr should return a distinct pointer on each call")
	}
}

// ---------------------------------------------------------------------------
// isRemoteHost
// ---------------------------------------------------------------------------

func TestIsRemoteHost_Localhost(t *testing.T) {
	if isRemoteHost("localhost") {
		t.Error(`isRemoteHost("localhost") = true, want false (localhost is local)`)
	}
}

func TestIsRemoteHost_Loopback(t *testing.T) {
	if isRemoteHost("127.0.0.1") {
		t.Error(`isRemoteHost("127.0.0.1") = true, want false (loopback is local)`)
	}
}

func TestIsRemoteHost_UnresolvableDomain(t *testing.T) {
	if !isRemoteHost("this-host-does-not-exist-xyz.invalid") {
		t.Error("isRemoteHost of unresolvable domain should return true (assume remote)")
	}
}

func TestIsRemoteHost_PublicResolverIP(t *testing.T) {
	// 8.8.8.8 resolves to itself and is not a local interface address.
	if !isRemoteHost("8.8.8.8") {
		t.Error(`isRemoteHost("8.8.8.8") = false, want true (not a local interface)`)
	}
}

func TestIsRemoteHost_InterfacesErrorAssumesRemote(t *testing.T) {
	old := netInterfacesForRemoteHost
	netInterfacesForRemoteHost = func() ([]net.Interface, error) {
		return nil, fmt.Errorf("simulated interfaces failure")
	}
	t.Cleanup(func() { netInterfacesForRemoteHost = old })
	if !isRemoteHost("127.0.0.1") {
		t.Error("isRemoteHost should assume remote when net.Interfaces fails")
	}
}

func TestIsRemoteHost_LookupHostErrorAssumesRemote(t *testing.T) {
	old := lookupHostFn
	lookupHostFn = func(string) ([]string, error) {
		return nil, fmt.Errorf("simulated lookup failure")
	}
	t.Cleanup(func() { lookupHostFn = old })
	if !isRemoteHost("any.example") {
		t.Error("isRemoteHost should assume remote when LookupHost fails")
	}
}

func TestIsRemoteHost_IfaceAddrsErrorLeavesLocalMapEmpty(t *testing.T) {
	old := ifaceAddrsFn
	ifaceAddrsFn = func(net.Interface) ([]net.Addr, error) {
		return nil, fmt.Errorf("simulated Addrs failure")
	}
	t.Cleanup(func() { ifaceAddrsFn = old })
	if !isRemoteHost("127.0.0.1") {
		t.Error("isRemoteHost should treat host as remote when no interface addrs are available")
	}
}

func TestIsRemoteHost_NonIPNetAddrTypesSkipped(t *testing.T) {
	old := ifaceAddrsFn
	ifaceAddrsFn = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443}}, nil
	}
	t.Cleanup(func() { ifaceAddrsFn = old })
	if !isRemoteHost("127.0.0.1") {
		t.Error("isRemoteHost should treat host as remote when only non-IPNet addrs are present")
	}
}

// TestIsRemoteHost_IPAddrTypeCountsAsLocal covers the *net.IPAddr branch in
// the address-type switch (not only *net.IPNet).
func TestIsRemoteHost_IPAddrTypeCountsAsLocal(t *testing.T) {
	oldLH := lookupHostFn
	lookupHostFn = func(string) ([]string, error) {
		return []string{"127.0.0.1"}, nil
	}
	t.Cleanup(func() { lookupHostFn = oldLH })
	oldIface := ifaceAddrsFn
	ifaceAddrsFn = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}}, nil
	}
	t.Cleanup(func() { ifaceAddrsFn = oldIface })
	if isRemoteHost("127.0.0.1") {
		t.Error(`isRemoteHost("127.0.0.1") = true, want false when *net.IPAddr matches LookupHost result`)
	}
}

// ---------------------------------------------------------------------------
// isoProgressTracker
// ---------------------------------------------------------------------------

// openProgressPipe creates an isoProgressTracker backed by an io.Pipe and
// returns a channel that receives all bytes written after the pipe is closed.
func openProgressPipe(totalSz int64) (*isoProgressTracker, chan []byte) {
	pr, pw := io.Pipe()
	tracker := &isoProgressTracker{pw: pw, fakeTotalSz: totalSz}
	ch := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, pr)
		ch <- buf.Bytes()
	}()
	return tracker, ch
}

func TestIsoProgressTracker_Advance_WritesBytes(t *testing.T) {
	tracker, ch := openProgressPipe(100)
	tracker.advance(50)
	tracker.done()
	data := <-ch
	// advance(50) writes 50 bytes; done() advances from 50 to 100, writing another 50.
	if len(data) != 100 {
		t.Errorf("expected 100 bytes written, got %d", len(data))
	}
}

func TestIsoProgressTracker_Advance_NoOp_SamePct(t *testing.T) {
	tracker, ch := openProgressPipe(100)
	tracker.advance(30)
	tracker.advance(30) // duplicate — must be ignored
	tracker.done()
	data := <-ch
	if len(data) != 100 {
		t.Errorf("expected 100 bytes, got %d", len(data))
	}
}

func TestIsoProgressTracker_Advance_NoOp_BackwardPct(t *testing.T) {
	tracker, ch := openProgressPipe(100)
	tracker.advance(70)
	tracker.advance(40) // backward — must be ignored
	tracker.done()
	data := <-ch
	if len(data) != 100 {
		t.Errorf("expected 100 bytes, got %d", len(data))
	}
}

func TestIsoProgressTracker_AdvanceDeltaZeroWhenIncrementRoundsDown(t *testing.T) {
	tracker, ch := openProgressPipe(1)
	tracker.advance(1) // (1-0)*1/100 = 0 — delta branch, lastPct unchanged
	tracker.advance(100)
	tracker.done()
	data := <-ch
	if len(data) != 1 {
		t.Errorf("expected 1 byte total for fakeTotalSz=1, got %d", len(data))
	}
}

func TestIsoProgressTracker_Done_ClosesFromZero(t *testing.T) {
	tracker, ch := openProgressPipe(200)
	tracker.done()
	data := <-ch
	if len(data) != 200 {
		t.Errorf("done() from 0 should write all 200 bytes, got %d", len(data))
	}
}

func TestIsoProgressTracker_Cancel_ClosesWithError(t *testing.T) {
	pr, pw := io.Pipe()
	tracker := &isoProgressTracker{pw: pw, fakeTotalSz: 100}
	ch := make(chan error, 1)
	go func() {
		buf := make([]byte, 1)
		_, err := pr.Read(buf)
		ch <- err
	}()
	tracker.cancel()
	err := <-ch
	if err == nil {
		t.Error("cancel() should close the pipe with an error, got nil")
	}
}
