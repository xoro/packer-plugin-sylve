// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

// TestStepVNCBootCommand_ReconnectClosure_ReconnectsAfterRun calls the
// vnc_reconnect closure after a successful Run so the dial + handshake +
// swapConn path in the reconnect loop is exercised.
func TestStepVNCBootCommand_ReconnectClosure_ReconnectsAfterRun(t *testing.T) {
	orig := vncReconnectRetryDelay
	vncReconnectRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { vncReconnectRetryDelay = orig })

	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
	rfbAddr := rfbLn.Addr().String()

	go func() {
		for {
			c, err := rfbLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_ = serveMinimalRFB(conn)
			}(c)
		}
	}()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/vnc/") {
			http.NotFound(w, r)
			return
		}
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		tcpConn, err := net.Dial("tcp", rfbAddr)
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	cfg := &Config{
		SylveURL:        srv.URL,
		SylveToken:      "test-token",
		VNCPort:         5900,
		VNCHost:         "127.0.0.1",
		TLSSkipVerify:   true,
		BootWait:        "1ns",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", ln)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}

	rfn, ok := state.GetOk("vnc_reconnect")
	if !ok {
		t.Fatal("vnc_reconnect not in state")
	}
	reconnect, ok := rfn.(vncReconnectFunc)
	if !ok {
		t.Fatalf("vnc_reconnect has wrong type %T", rfn)
	}
	if err := reconnect(context.Background(), newMockUI()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
}

// TestStepVNCBootCommand_ReconnectClosure_ContextCancelledOnDialFailure exercises
// the reconnect loop when the WebSocket dial fails and the context is already
// cancelled (no long retry delay).
func TestStepVNCBootCommand_ReconnectClosure_ContextCancelledOnDialFailure(t *testing.T) {
	orig := vncReconnectRetryDelay
	vncReconnectRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { vncReconnectRetryDelay = orig })

	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
	rfbAddr := rfbLn.Addr().String()

	go func() {
		for {
			c, err := rfbLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_ = serveMinimalRFB(conn)
			}(c)
		}
	}()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/vnc/") {
			http.NotFound(w, r)
			return
		}
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		tcpConn, err := net.Dial("tcp", rfbAddr)
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	cfg := &Config{
		SylveURL:        srv.URL,
		SylveToken:      "test-token",
		VNCPort:         5900,
		VNCHost:         "127.0.0.1",
		TLSSkipVerify:   true,
		BootWait:        "1ns",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", ln)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}

	srv.Close()

	rfn := state.Get("vnc_reconnect").(vncReconnectFunc)
	rctx, rcancel := context.WithCancel(context.Background())
	rcancel()
	if err := rfn(rctx, newMockUI()); err == nil {
		t.Fatal("expected error from cancelled reconnect context")
	}
}

// TestStepVNCBootCommand_ReconnectClosure_VNCHandshakeRetriesBeforeSuccess
// exercises the reconnect loop when the TCP side accepts but closes before
// RFB completes, then a subsequent attempt gets a full minimal RFB server.
func TestStepVNCBootCommand_ReconnectClosure_VNCHandshakeRetriesBeforeSuccess(t *testing.T) {
	orig := vncReconnectRetryDelay
	vncReconnectRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { vncReconnectRetryDelay = orig })

	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
	rfbAddr := rfbLn.Addr().String()

	var acceptN int32
	go func() {
		for {
			c, err := rfbLn.Accept()
			if err != nil {
				return
			}
			n := atomic.AddInt32(&acceptN, 1)
			if n == 1 {
				go func(conn net.Conn) {
					defer conn.Close()
					_ = serveMinimalRFB(conn)
				}(c)
				continue
			}
			if n == 2 {
				_ = c.Close()
				continue
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_ = serveMinimalRFB(conn)
			}(c)
		}
	}()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/vnc/") {
			http.NotFound(w, r)
			return
		}
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		tcpConn, err := net.Dial("tcp", rfbAddr)
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	cfg := &Config{
		SylveURL:        srv.URL,
		SylveToken:      "test-token",
		VNCPort:         5900,
		VNCHost:         "127.0.0.1",
		TLSSkipVerify:   true,
		BootWait:        "1ns",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", ln)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}

	rfn := state.Get("vnc_reconnect").(vncReconnectFunc)
	if err := rfn(context.Background(), newMockUI()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if atomic.LoadInt32(&acceptN) < 3 {
		t.Fatalf("expected at least 3 RFB accepts (initial + failed reconnect + success), got %d", acceptN)
	}
}

// TestStepVNCBootCommand_Run_InvalidUTF8BootCommandHalts covers
// GenerateExpressionSequence failure after the VNC session is established.
func TestStepVNCBootCommand_Run_InvalidUTF8BootCommandHalts(t *testing.T) {
	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
	rfbAddr := rfbLn.Addr().String()

	go func() {
		c, err := rfbLn.Accept()
		if err != nil {
			return
		}
		_ = serveMinimalRFB(c)
		_ = c.Close()
	}()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/vnc/") {
			http.NotFound(w, r)
			return
		}
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		tcpConn, err := net.Dial("tcp", rfbAddr)
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:        srv.URL,
		SylveToken:      "test-token",
		VNCPort:         5900,
		VNCHost:         "127.0.0.1",
		TLSSkipVerify:   true,
		BootWait:        "1ns",
		BootCommand:     []string{string([]byte{0xff, 0xfe})},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}
