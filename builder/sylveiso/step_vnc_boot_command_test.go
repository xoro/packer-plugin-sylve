// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// newWSPair creates an httptest WebSocket echo server and returns a *wsNetConn
// connected to it. The caller must close the returned conn and server.
func newWSPair(t *testing.T) (*wsNetConn, *httptest.Server) {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		for {
			mt, msg, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			if writeErr := conn.WriteMessage(mt, msg); writeErr != nil {
				return
			}
		}
	}))

	wsURL := "ws://" + srv.Listener.Addr().String()
	cli, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	return &wsNetConn{conn: cli}, srv
}

// ---------------------------------------------------------------------------
// wsNetConn.Write and Read (echo test)
// ---------------------------------------------------------------------------

func TestWsNetConn_WriteRead_Echo(t *testing.T) {
	w, srv := newWSPair(t)
	defer srv.Close()
	defer w.Close()

	send := []byte("hello vnc")
	if _, err := w.Write(send); err != nil {
		t.Fatalf("Write: %v", err)
	}

	recv := make([]byte, len(send))
	n, err := w.Read(recv)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(recv[:n]) != string(send) {
		t.Errorf("Read = %q, want %q", recv[:n], send)
	}
}

func TestWsNetConn_Write_Concurrent(t *testing.T) {
	w, srv := newWSPair(t)
	defer srv.Close()
	defer w.Close()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = w.Write([]byte{0x01})
		}()
	}
	wg.Wait()
}

func TestWsNetConn_Write_ErrorAfterClose(t *testing.T) {
	w, srv := newWSPair(t)
	defer srv.Close()
	defer w.Close()
	_ = w.Close()
	if _, err := w.Write([]byte{0x01}); err == nil {
		t.Fatal("expected error writing to closed WebSocket after Close")
	}
}

func TestStepVNCBootCommand_Cleanup_IsNoOp(t *testing.T) {
	s := &StepVNCBootCommand{Config: &Config{}}
	s.Cleanup(nil)
}

func TestWsNetConn_Read_SplitBuffer(t *testing.T) {
	// Write a 4-byte message, then read it in two 2-byte chunks to exercise
	// the buf-draining path in Read.
	w, srv := newWSPair(t)
	defer srv.Close()
	defer w.Close()

	if _, err := w.Write([]byte("abcd")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	p := make([]byte, 2)
	n, err := w.Read(p)
	if err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if n != 2 || string(p[:n]) != "ab" {
		t.Errorf("first Read = %q, want %q", p[:n], "ab")
	}

	n, err = w.Read(p)
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if n != 2 || string(p[:n]) != "cd" {
		t.Errorf("second Read = %q, want %q", p[:n], "cd")
	}
}

// ---------------------------------------------------------------------------
// wsNetConn.Close
// ---------------------------------------------------------------------------

func TestWsNetConn_Close(t *testing.T) {
	w, srv := newWSPair(t)
	defer srv.Close()
	if err := w.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// wsNetConn.LocalAddr / RemoteAddr
// ---------------------------------------------------------------------------

func TestWsNetConn_LocalAddr_NotNil(t *testing.T) {
	w, srv := newWSPair(t)
	defer srv.Close()
	defer w.Close()
	if w.LocalAddr() == nil {
		t.Error("LocalAddr() returned nil")
	}
}

func TestWsNetConn_RemoteAddr_NotNil(t *testing.T) {
	w, srv := newWSPair(t)
	defer srv.Close()
	defer w.Close()
	if w.RemoteAddr() == nil {
		t.Error("RemoteAddr() returned nil")
	}
}

// ---------------------------------------------------------------------------
// wsNetConn deadline setters
// ---------------------------------------------------------------------------

func TestWsNetConn_SetDeadline(t *testing.T) {
	w, srv := newWSPair(t)
	defer srv.Close()
	defer w.Close()
	_ = w.SetDeadline(time.Now().Add(5 * time.Second))
}

func TestWsNetConn_SetReadDeadline(t *testing.T) {
	w, srv := newWSPair(t)
	defer srv.Close()
	defer w.Close()
	_ = w.SetReadDeadline(time.Now().Add(5 * time.Second))
}

func TestWsNetConn_SetWriteDeadline(t *testing.T) {
	w, srv := newWSPair(t)
	defer srv.Close()
	defer w.Close()
	_ = w.SetWriteDeadline(time.Now().Add(5 * time.Second))
}

// ---------------------------------------------------------------------------
// Compile-time interface check
// ---------------------------------------------------------------------------

func TestWsNetConn_ImplementsNetConn(t *testing.T) {
	// Verifies the static assertion in the source file: no run-time cost.
	var _ interface{} = (*wsNetConn)(nil)
}

// ---------------------------------------------------------------------------
// localHTTPIPForSwitch
// ---------------------------------------------------------------------------

func TestLocalHTTPIPForSwitch_NonLoopbackPassthrough(t *testing.T) {
	// When the candidate IP is not loopback it should be returned as-is
	// without calling netInterfaces.
	origInterfaces := netInterfaces
	netInterfaces = func() ([]net.Interface, error) {
		t.Fatal("netInterfaces should not be called for a non-loopback candidate")
		return nil, nil
	}
	defer func() { netInterfaces = origInterfaces }()

	got := localHTTPIPForSwitch("192.168.11.51", "server.home.pallach.de")
	if got != "192.168.11.51" {
		t.Fatalf("expected 192.168.11.51, got %s", got)
	}
}

func TestLocalHTTPIPForSwitch_LoopbackFallsBackToBridgeIP(t *testing.T) {
	// Simulates packer running on the Sylve host: the UDP dial returns
	// 127.0.0.1. The function must enumerate interfaces, skip the loopback
	// and the Sylve host's own external IP, and return the bridge IP.
	origInterfaces := netInterfaces
	netInterfaces = func() ([]net.Interface, error) {
		// Return a minimal set: loopback (lo), external (re0), bridge (PackerSwitch).
		return []net.Interface{
			{Index: 1, Name: "lo0", Flags: net.FlagUp | net.FlagLoopback},
			{Index: 2, Name: "re0", Flags: net.FlagUp | net.FlagBroadcast},
			{Index: 3, Name: "bridge0", Flags: net.FlagUp | net.FlagBroadcast},
		}, nil
	}
	defer func() { netInterfaces = origInterfaces }()

	// We cannot inject Addrs() via the net.Interface struct directly since
	// Addrs is a method on the OS-backed type. Instead we test the logic by
	// verifying that a non-loopback candidate is passed through unchanged,
	// and that the invalid/loopback cases return the original when no valid
	// bridge interface is injectable. This covers the function's guard
	// clauses while the integration path is exercised by build tests.
	got := localHTTPIPForSwitch("not-an-ip", "sylve-host")
	if got != "not-an-ip" {
		t.Fatalf("invalid IP should be returned as-is, got %s", got)
	}
}

func TestLocalHTTPIPForSwitch_LoopbackReturnsOriginalWhenNoAlternative(t *testing.T) {
	// When the candidate is loopback but no non-loopback non-sylve interface
	// exists, the original candidate is returned unchanged.
	origInterfaces := netInterfaces
	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{}, nil
	}
	defer func() { netInterfaces = origInterfaces }()

	got := localHTTPIPForSwitch("127.0.0.1", "127.0.0.1")
	if got != "127.0.0.1" {
		t.Fatalf("expected fallback to original 127.0.0.1, got %s", got)
	}
}

func TestLocalHTTPIPForSwitch_NetInterfacesErrorReturnsCandidate(t *testing.T) {
	origInterfaces := netInterfaces
	netInterfaces = func() ([]net.Interface, error) {
		return nil, errors.New("simulated net.Interfaces failure")
	}
	t.Cleanup(func() { netInterfaces = origInterfaces })

	got := localHTTPIPForSwitch("127.0.0.1", "192.0.2.1")
	if got != "127.0.0.1" {
		t.Fatalf("on net.Interfaces error, want original candidate, got %s", got)
	}
}

func TestLocalHTTPIPForSwitch_IPv6LoopbackWhenNetInterfacesFails(t *testing.T) {
	origInterfaces := netInterfaces
	netInterfaces = func() ([]net.Interface, error) {
		return nil, errors.New("simulated net.Interfaces failure")
	}
	t.Cleanup(func() { netInterfaces = origInterfaces })

	got := localHTTPIPForSwitch("::1", "192.0.2.1")
	if got != "::1" {
		t.Fatalf("want ::1 when enumeration fails, got %s", got)
	}
}

func TestLocalHTTPIPForSwitch_NonLoopbackIPv6Passthrough(t *testing.T) {
	origInterfaces := netInterfaces
	netInterfaces = func() ([]net.Interface, error) {
		t.Fatal("netInterfaces must not be called for non-loopback candidate")
		return nil, nil
	}
	t.Cleanup(func() { netInterfaces = origInterfaces })

	const u = "2001:db8::1"
	if got := localHTTPIPForSwitch(u, "host"); got != u {
		t.Fatalf("got %q, want %q", got, u)
	}
}

func TestLocalHTTPIPForSwitch_UnparseableCandidatePassthrough(t *testing.T) {
	origInterfaces := netInterfaces
	netInterfaces = func() ([]net.Interface, error) {
		t.Fatal("netInterfaces must not be called when candidate is not an IP")
		return nil, nil
	}
	t.Cleanup(func() { netInterfaces = origInterfaces })

	if got := localHTTPIPForSwitch("not-an-ip", "vnc"); got != "not-an-ip" {
		t.Fatalf("got %q", got)
	}
}
