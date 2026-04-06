// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"errors"
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

// TestStepVNCBootCommand_Run_vncHandshakeRetryAfterBadRFB exercises the inner
// retry loop when WebSocket connects but the first TCP segment is not a valid
// RFB banner (VNC handshake fails, then succeeds on a fresh connection).
func TestStepVNCBootCommand_Run_vncHandshakeRetryAfterBadRFB(t *testing.T) {
	orig := vncStepDialRetryDelay
	vncStepDialRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { vncStepDialRetryDelay = orig })
	var rfbRound int32
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
			n := atomic.AddInt32(&rfbRound, 1)
			if n == 1 {
				_, _ = c.Write([]byte("not-rfb-banner\n"))
				_ = c.Close()
				continue
			}
			_ = serveMinimalRFB(c)
			_ = c.Close()
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
	if err, ok := state.GetOk("error"); ok {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&rfbRound) < 2 {
		t.Fatalf("expected 2 RFB server connections, got %d", rfbRound)
	}
}

// TestStepVNCBootCommand_Run_ContextCancelDuringHandshakeRetry stops when the
// build context is cancelled while retrying a failed VNC handshake (WebSocket
// connected but RFB never completes).
func TestStepVNCBootCommand_Run_ContextCancelDuringHandshakeRetry(t *testing.T) {
	origD := vncStepDialRetryDelay
	origO := vncStepOverallDeadline
	vncStepDialRetryDelay = 1 * time.Millisecond
	vncStepOverallDeadline = 30 * time.Second
	t.Cleanup(func() {
		vncStepDialRetryDelay = origD
		vncStepOverallDeadline = origO
	})

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
			_, _ = c.Write([]byte("not-a-valid-rfb-banner\n"))
			_ = c.Close()
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

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	err, ok := state.Get("error").(error)
	if !ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", state.Get("error"))
	}
}
