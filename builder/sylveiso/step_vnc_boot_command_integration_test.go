// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

// TestStepVNCBootCommand_Run_vncProxy exercises the full StepVNCBootCommand path
// against a local HTTPS server that upgrades to WebSocket and bridges to a
// minimal RFB 3.8 server (password auth + framebuffer updates).
func TestStepVNCBootCommand_Run_vncProxy(t *testing.T) {
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

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	viewPort := ln.Addr().(*net.TCPAddr).Port
	var wgViewer sync.WaitGroup
	wgViewer.Add(1)
	go func() {
		defer wgViewer.Done()
		time.Sleep(400 * time.Millisecond)
		cli, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", viewPort))
		if err != nil {
			t.Errorf("dial view server: %v", err)
			return
		}
		defer cli.Close()
		vncClientCompleteRFB38Handshake(t, cli)
		if _, err := cli.Write([]byte{3, 0, 0, 0, 0, 0, 0, 4, 0, 4}); err != nil {
			t.Errorf("FBU request: %v", err)
			return
		}
		readFramebufferUpdateRawPixels(t, cli, 4, 4)
	}()

	cfg := &Config{
		SylveURL:        srv.URL,
		SylveToken:      "test-token",
		VNCPort:         5900,
		VNCHost:         "127.0.0.1",
		TLSSkipVerify:   true,
		BootWait:        "1200ms",
		BootCommand:     []string{"<enter>"},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("http_ip", "10.11.12.13")
	state.Put("http_port", 8080)
	state.Put("vnc_view_listener", ln)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if err, ok := state.GetOk("error"); ok {
		t.Fatalf("unexpected error in state: %v", err)
	}

	doneViewer := make(chan struct{})
	go func() {
		wgViewer.Wait()
		close(doneViewer)
	}()
	select {
	case <-doneViewer:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for VNC viewer goroutine")
	}
}

// TestStepVNCBootCommand_Run_NoViewListenerBranch hits the branch where the
// view-server listener is absent (only the Sylve proxy path runs).
func TestStepVNCBootCommand_Run_NoViewListenerBranch(t *testing.T) {
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
		BootWait:        "200ms",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
}

// TestStepVNCBootCommand_Run_DerivesHTTPIPFromUDPWhenUnset exercises the UDP
// dial path and HTTPPort template data when http_ip is not pre-seeded.
func TestStepVNCBootCommand_Run_DerivesHTTPIPFromUDPWhenUnset(t *testing.T) {
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
		BootWait:        "200ms",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
		VMName:          "tpl-vm",
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("http_port", 9000)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	ip, ok := state.Get("http_ip").(string)
	if !ok || ip == "" {
		t.Fatalf("expected http_ip derived via UDP, got %q ok=%v", ip, ok)
	}
}

// TestStepVNCBootCommand_Run_UDPDialFailsLeavesHTTPIPUnset covers the branch
// where deriving http_ip from a UDP dial fails (nothing is written to state).
func TestStepVNCBootCommand_Run_UDPDialFailsLeavesHTTPIPUnset(t *testing.T) {
	old := netDialUDPForHTTPIP
	netDialUDPForHTTPIP = func(string, string) (net.Conn, error) {
		return nil, fmt.Errorf("simulated udp dial failure")
	}
	t.Cleanup(func() { netDialUDPForHTTPIP = old })

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
		BootWait:        "200ms",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if _, ok := state.GetOk("http_ip"); ok {
		t.Fatal("expected http_ip absent when UDP dial is stubbed to fail")
	}
}

// TestStepVNCBootCommand_Run_ParseSecondBootCommandFails covers GenerateExpressionSequence
// failure on a later boot_command after the VNC session is up.
func TestStepVNCBootCommand_Run_ParseSecondBootCommandFails(t *testing.T) {
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

	cfg := &Config{
		SylveURL:        srv.URL,
		SylveToken:      "test-token",
		VNCPort:         5900,
		VNCHost:         "127.0.0.1",
		TLSSkipVerify:   true,
		BootWait:        "1ns",
		BootCommand:     []string{"<wait1ms>", string([]byte{0xff, 0xfe})},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}

// TestStepVNCBootCommand_Run_ContextCancelDuringBootCommand cancels the build
// context while a boot_command <waitXs> is in progress.
func TestStepVNCBootCommand_Run_ContextCancelDuringBootCommand(t *testing.T) {
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

	cfg := &Config{
		SylveURL:        srv.URL,
		SylveToken:      "test-token",
		VNCPort:         5900,
		VNCHost:         "127.0.0.1",
		TLSSkipVerify:   true,
		BootWait:        "1ns",
		BootCommand:     []string{"<wait2s>"},
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

// TestStepVNCBootCommand_Run_plainHTTPSylveURLUsesWsDial exercises the
// http:// to ws:// SylveURL rewrite when Sylve is configured without TLS.
func TestStepVNCBootCommand_Run_plainHTTPSylveURLUsesWsDial(t *testing.T) {
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		TLSSkipVerify:   false,
		BootWait:        "200ms",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if got := step.Run(ctx, state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
}

// TestStepVNCBootCommand_ContextCancelDuringBootWait stops during the boot_wait
// sleep after the VNC session is established (listener optional).
func TestStepVNCBootCommand_ContextCancelDuringBootWait(t *testing.T) {
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
		BootWait:        "2s",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	if state.Get("error") != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", state.Get("error"))
	}
}

// bridgeWebSocketTCP copies bytes between a gorilla WebSocket and a TCP conn
// (same layout as Sylve's proxy: one binary message per VNC write chunk).
func bridgeWebSocketTCP(ws *websocket.Conn, tcp net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if _, err := tcp.Write(msg); err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 64*1024)
		for {
			n, err := tcp.Read(buf)
			if err != nil {
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()
	wg.Wait()
	_ = tcp.Close()
	_ = ws.Close()
}

// serveMinimalRFB implements RFB 3.8 with security type 2 (VNC password) and
// answers FramebufferUpdateRequest messages with a minimal raw-encoded update.
func serveMinimalRFB(conn net.Conn) error {
	const fbW, fbH = 64, 64

	if _, err := conn.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	ver := make([]byte, 12)
	if _, err := io.ReadFull(conn, ver); err != nil {
		return err
	}

	// Offer only VNC password auth (type 2), matching PasswordAuth in the client.
	if _, err := conn.Write([]byte{1, 2}); err != nil {
		return err
	}
	choice := make([]byte, 1)
	if _, err := io.ReadFull(conn, choice); err != nil {
		return err
	}
	challenge := make([]byte, 16)
	for i := range challenge {
		challenge[i] = byte(i)
	}
	if _, err := conn.Write(challenge); err != nil {
		return err
	}
	resp := make([]byte, 16)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return err
	}
	if err := binary.Write(conn, binary.BigEndian, uint32(0)); err != nil { // SecurityResult OK
		return err
	}

	shared := make([]byte, 1)
	if _, err := io.ReadFull(conn, shared); err != nil {
		return err
	}

	// ServerInit — same 32bpp layout as vnc_view_server.handleVNCClient.
	pixFmt := [16]byte{
		32, 24, 0, 1,
		0, 255, 0, 255, 0, 255,
		16, 8, 0,
		0, 0, 0,
	}
	name := []byte("test")
	var wbuf []byte
	wbuf = append(wbuf, byte(fbW>>8), byte(fbW), byte(fbH>>8), byte(fbH))
	wbuf = append(wbuf, pixFmt[:]...)
	nl := uint32(len(name))
	wbuf = append(wbuf, byte(nl>>24), byte(nl>>16), byte(nl>>8), byte(nl))
	wbuf = append(wbuf, name...)
	if _, err := conn.Write(wbuf); err != nil {
		return err
	}

	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(conn, buf); err != nil {
			return err
		}
		switch buf[0] {
		case 0: // SetPixelFormat
			rest := make([]byte, 19)
			if _, err := io.ReadFull(conn, rest); err != nil {
				return err
			}
		case 2: // SetEncodings
			hdr := make([]byte, 3)
			if _, err := io.ReadFull(conn, hdr); err != nil {
				return err
			}
			count := int(binary.BigEndian.Uint16(hdr[1:3]))
			encs := make([]byte, count*4)
			if _, err := io.ReadFull(conn, encs); err != nil {
				return err
			}
		case 3: // FramebufferUpdateRequest
			rest := make([]byte, 9)
			if _, err := io.ReadFull(conn, rest); err != nil {
				return err
			}
			if err := sendMinimalFramebufferUpdate(conn, fbW, fbH); err != nil {
				return err
			}
		case 4: // KeyEvent
			rest := make([]byte, 7)
			if _, err := io.ReadFull(conn, rest); err != nil {
				return err
			}
		case 5: // PointerEvent
			rest := make([]byte, 5)
			if _, err := io.ReadFull(conn, rest); err != nil {
				return err
			}
		case 6: // ClientCutText
			hdr := make([]byte, 7)
			if _, err := io.ReadFull(conn, hdr); err != nil {
				return err
			}
			length := binary.BigEndian.Uint32(hdr[3:7])
			if length > 0 {
				text := make([]byte, length)
				if _, err := io.ReadFull(conn, text); err != nil {
					return err
				}
			}
		default:
			return io.EOF
		}
	}
}

func sendMinimalFramebufferUpdate(conn net.Conn, w, h uint16) error {
	hdr := []byte{0, 0, 0, 1}
	if _, err := conn.Write(hdr); err != nil {
		return err
	}
	rect := make([]byte, 12)
	binary.BigEndian.PutUint16(rect[0:2], 0)
	binary.BigEndian.PutUint16(rect[2:4], 0)
	binary.BigEndian.PutUint16(rect[4:6], w)
	binary.BigEndian.PutUint16(rect[6:8], h)
	binary.BigEndian.PutUint32(rect[8:12], 0)
	if _, err := conn.Write(rect); err != nil {
		return err
	}
	n := int(w) * int(h) * 4
	pixels := make([]byte, n)
	_, err := conn.Write(pixels)
	return err
}
