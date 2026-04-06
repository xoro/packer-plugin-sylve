// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"net"
	"testing"
	"time"

	vnc "github.com/mitchellh/go-vnc"
)

// TestSwapConn_ReplacesConn exercises swapConn: framebuffer resize, notify
// hooks, and a short second framebuffer poller loop.
func TestSwapConn_ReplacesConn(t *testing.T) {
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln1.Close()
	go func() {
		c, err := ln1.Accept()
		if err != nil {
			return
		}
		_ = serveMinimalRFB(c)
		_ = c.Close()
	}()

	c1, err := net.Dial("tcp", ln1.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()

	msgCh1 := make(chan vnc.ServerMessage, 200)
	vncCfg1 := &vnc.ClientConfig{
		Exclusive:       false,
		ServerMessageCh: msgCh1,
		Auth:            []vnc.ClientAuth{&vnc.PasswordAuth{}},
	}
	client1, err := vnc.Client(c1, vncCfg1)
	if err != nil {
		t.Fatalf("vnc.Client: %v", err)
	}
	defer client1.Close()

	ss := newVNCViewServer(client1)

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()
	go func() {
		c, err := ln2.Accept()
		if err != nil {
			return
		}
		_ = serveMinimalRFB(c)
		_ = c.Close()
	}()

	c2, err := net.Dial("tcp", ln2.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	msgCh2 := make(chan vnc.ServerMessage, 200)
	vncCfg2 := &vnc.ClientConfig{
		Exclusive:       false,
		ServerMessageCh: msgCh2,
		Auth:            []vnc.ClientAuth{&vnc.PasswordAuth{}},
	}
	client2, err := vnc.Client(c2, vncCfg2)
	if err != nil {
		t.Fatalf("vnc.Client 2: %v", err)
	}
	defer client2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	ss.swapConn(ctx, client2, msgCh2)

	// Let the poller run multiple ticker cycles (500ms) and process server messages.
	time.Sleep(2100 * time.Millisecond)
}
