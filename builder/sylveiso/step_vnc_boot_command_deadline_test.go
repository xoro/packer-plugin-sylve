// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

// TestStepVNCBootCommand_Run_WsDialExhaustsDeadline covers the dial retry loop
// when the WebSocket endpoint never upgrades and the overall deadline expires.
func TestStepVNCBootCommand_Run_WsDialExhaustsDeadline(t *testing.T) {
	origD := vncStepDialRetryDelay
	origO := vncStepOverallDeadline
	vncStepDialRetryDelay = 1 * time.Millisecond
	vncStepOverallDeadline = 25 * time.Millisecond
	t.Cleanup(func() {
		vncStepDialRetryDelay = origD
		vncStepOverallDeadline = origO
	})

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
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

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}

// TestStepVNCBootCommand_Run_VNCHandshakeExhaustsDeadline covers the branch
// where WebSocket connects but VNC handshake never succeeds before the deadline.
func TestStepVNCBootCommand_Run_VNCHandshakeExhaustsDeadline(t *testing.T) {
	origD := vncStepDialRetryDelay
	origO := vncStepOverallDeadline
	vncStepDialRetryDelay = 1 * time.Millisecond
	vncStepOverallDeadline = 40 * time.Millisecond
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

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}
