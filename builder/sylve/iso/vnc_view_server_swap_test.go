// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package iso

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

// TestSwapConn_RejectsConnWhenStopped verifies that swapConn closes newConn and
// returns without installing it when Stop() has already run. A reconnect
// goroutine can race with Stop(): after Stop sets ss.stopped=true and
// ss.conn=nil, the reconnect finishes dialing and calls swapConn. Without the
// stopped guard swapConn would install newConn, the new poller would exit
// immediately (stopped=true), and the go-vnc reader goroutine would fill
// serverMsgCh with FramebufferUpdateMessage objects (~4 MB each, channel
// capacity 200) that nobody drains — bloating the heap to ~800 MB and causing
// sustained GC at 800%+ CPU.
func TestSwapConn_RejectsConnWhenStopped(t *testing.T) {
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
		t.Fatalf("vnc.Client 1: %v", err)
	}
	defer client1.Close()

	ss := newVNCViewServer(client1)

	// Stop the view server before swapConn runs. This simulates Stop() winning
	// the race against a reconnect goroutine that is already in-flight.
	ss.Stop()

	// Second connection: simulates the reconnect goroutine completing its dial
	// after Stop() has already set stopped=true.
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
	// No defer Close — swapConn must close it.

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// swapConn must reject client2 because ss.stopped=true.
	ss.swapConn(ctx, client2, msgCh2)

	// The connection must be closed: a write should now fail.
	err = client2.FramebufferUpdateRequest(false, 0, 0, 100, 100)
	if err == nil {
		t.Error("expected FramebufferUpdateRequest to fail on a connection closed by swapConn, got nil")
	}

	// ss.conn must remain nil — Stop() set it to nil and swapConn must not restore it.
	ss.mu.RLock()
	conn := ss.conn
	ss.mu.RUnlock()
	if conn != nil {
		t.Error("expected ss.conn to remain nil after swapConn rejects a stopped view server")
	}
}
