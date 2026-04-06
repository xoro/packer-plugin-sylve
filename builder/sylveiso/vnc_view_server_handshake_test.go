// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	vnc "github.com/mitchellh/go-vnc"
)

// vncClientCompleteRFB38Handshake reads the server banner, completes the RFB 3.8
// handshake up to and including ServerInit, leaving c ready for client-to-server messages.
func vncClientCompleteRFB38Handshake(t *testing.T, c net.Conn) {
	t.Helper()
	banner := make([]byte, 12)
	if _, err := io.ReadFull(c, banner); err != nil {
		t.Fatalf("read banner: %v", err)
	}
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		t.Fatalf("write client version: %v", err)
	}
	sec := make([]byte, 2)
	if _, err := io.ReadFull(c, sec); err != nil {
		t.Fatalf("read security types: %v", err)
	}
	if _, err := c.Write([]byte{1}); err != nil {
		t.Fatalf("write security choice: %v", err)
	}
	if _, err := io.ReadFull(c, make([]byte, 4)); err != nil {
		t.Fatalf("read security result: %v", err)
	}
	if _, err := c.Write([]byte{0}); err != nil {
		t.Fatalf("write client init: %v", err)
	}
	fixed := make([]byte, 24)
	if _, err := io.ReadFull(c, fixed); err != nil {
		t.Fatalf("read server init fixed: %v", err)
	}
	nlen := binary.BigEndian.Uint32(fixed[20:24])
	name := make([]byte, nlen)
	if _, err := io.ReadFull(c, name); err != nil {
		t.Fatalf("read desktop name: %v", err)
	}
}

func readFramebufferUpdateRawPixels(t *testing.T, c net.Conn, w, h int) {
	t.Helper()
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		t.Fatalf("read FBU header: %v", err)
	}
	rect := make([]byte, 12)
	if _, err := io.ReadFull(c, rect); err != nil {
		t.Fatalf("read rect header: %v", err)
	}
	pix := make([]byte, w*h*4)
	if _, err := io.ReadFull(c, pix); err != nil {
		t.Fatalf("read pixels: %v", err)
	}
}

func TestHandleVNCClient_MessageTypesAfterHandshake(t *testing.T) {
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	ss.conn = nil

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ss.start(ctx, ln)

	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	vncClientCompleteRFB38Handshake(t, cli)

	// FramebufferUpdateRequest — full 4×4, non-incremental.
	if _, err := cli.Write([]byte{3, 0, 0, 0, 0, 0, 0, 4, 0, 4}); err != nil {
		t.Fatal(err)
	}
	readFramebufferUpdateRawPixels(t, cli, 4, 4)

	// SetPixelFormat (19 bytes padding + format).
	if _, err := cli.Write(append([]byte{0}, make([]byte, 19)...)); err != nil {
		t.Fatal(err)
	}

	// SetEncodings: padding + count=0.
	if _, err := cli.Write([]byte{2, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}

	// PointerEvent.
	if _, err := cli.Write([]byte{5, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}

	// KeyEvent (upstream nil — branch skipped).
	if _, err := cli.Write([]byte{4, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}

	// ClientCutText length 0 (type byte + 7-byte header).
	if _, err := cli.Write([]byte{6, 0, 0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}

	// Incremental FBU — unblock waitUpdated without waiting 500ms.
	go func() {
		time.Sleep(20 * time.Millisecond)
		ss.notifyUpdated()
	}()
	if _, err := cli.Write([]byte{3, 1, 0, 0, 0, 0, 0, 2, 0, 2}); err != nil {
		t.Fatal(err)
	}
	readFramebufferUpdateRawPixels(t, cli, 2, 2)

	// Unknown message type — handler returns.
	if _, err := cli.Write([]byte{0xff}); err != nil {
		t.Fatal(err)
	}
}
