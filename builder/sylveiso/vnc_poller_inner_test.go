// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	vnc "github.com/mitchellh/go-vnc"
)

// wireClientConnForTest assigns the unexported net.Conn field on *vnc.ClientConn
// (first struct field in github.com/mitchellh/go-vnc) so FramebufferUpdateRequest
// writes go to the given connection. Test-only.
func wireClientConnForTest(c *vnc.ClientConn, wire net.Conn) {
	type connHead struct {
		c net.Conn
	}
	p := (*connHead)(unsafe.Pointer(c))
	p.c = wire
}

// noopServerMsg is a non-FramebufferUpdate ServerMessage for poller branch coverage.
type noopServerMsg struct{}

func (noopServerMsg) Type() uint8 { return 7 }

func (noopServerMsg) Read(*vnc.ClientConn, io.Reader) (vnc.ServerMessage, error) {
	return nil, nil
}

func TestRunFramebufferPollerInner_ContextCancelled(t *testing.T) {
	r, w := net.Pipe()
	defer r.Close()
	defer w.Close()
	go func() { _, _ = io.Copy(io.Discard, r) }()

	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	wireClientConnForTest(conn, w)
	ss := newVNCViewServer(conn)

	ch := make(chan vnc.ServerMessage)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ss.runFramebufferPollerInner(ctx, ch, true)
}

func TestRunFramebufferPollerInner_ChannelCloseTriggersReconnect(t *testing.T) {
	r, w := net.Pipe()
	defer r.Close()
	defer w.Close()
	go func() { _, _ = io.Copy(io.Discard, r) }()

	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	wireClientConnForTest(conn, w)
	ss := newVNCViewServer(conn)

	var reconnectCalled int32
	ss.reconnect = vncReconnectFunc(func(context.Context, packersdk.Ui) error {
		atomic.AddInt32(&reconnectCalled, 1)
		return nil
	})
	ss.ui = newMockUI()

	ch := make(chan vnc.ServerMessage)
	close(ch)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ss.runFramebufferPollerInner(ctx, ch, true)

	time.Sleep(200 * time.Millisecond) // reconnect runs in a goroutine
	if atomic.LoadInt32(&reconnectCalled) != 1 {
		t.Fatalf("reconnect calls = %d, want 1", atomic.LoadInt32(&reconnectCalled))
	}
}

func TestRunFramebufferPollerInner_ChannelCloseNoAutoReconnect(t *testing.T) {
	r, w := net.Pipe()
	defer r.Close()
	defer w.Close()
	go func() { _, _ = io.Copy(io.Discard, r) }()

	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	wireClientConnForTest(conn, w)
	ss := newVNCViewServer(conn)

	var reconnectCalled int32
	ss.reconnect = vncReconnectFunc(func(context.Context, packersdk.Ui) error {
		atomic.AddInt32(&reconnectCalled, 1)
		return nil
	})
	ss.ui = newMockUI()

	ch := make(chan vnc.ServerMessage)
	close(ch)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ss.runFramebufferPollerInner(ctx, ch, false)

	if atomic.LoadInt32(&reconnectCalled) != 0 {
		t.Fatalf("reconnect calls = %d, want 0", atomic.LoadInt32(&reconnectCalled))
	}
}

func TestRunFramebufferPollerInner_NonFramebufferMessageIgnored(t *testing.T) {
	r, w := net.Pipe()
	defer r.Close()
	defer w.Close()
	go func() { _, _ = io.Copy(io.Discard, r) }()

	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	wireClientConnForTest(conn, w)
	ss := newVNCViewServer(conn)
	ss.ui = newMockUI()

	ch := make(chan vnc.ServerMessage, 1)
	ch <- noopServerMsg{}

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	ss.runFramebufferPollerInner(ctx, ch, false)
}

func TestRunFramebufferPollerInner_TickerFramebufferUpdateErrorReconnects(t *testing.T) {
	r, w := net.Pipe()
	// Do not defer Close immediately — close writer after poller issues ticker FBU.

	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	wireClientConnForTest(conn, w)
	ss := newVNCViewServer(conn)

	var reconnectCalled int32
	ss.reconnect = vncReconnectFunc(func(context.Context, packersdk.Ui) error {
		atomic.AddInt32(&reconnectCalled, 1)
		return nil
	})
	ss.ui = newMockUI()

	go func() {
		_, _ = io.Copy(io.Discard, r)
	}()

	ch := make(chan vnc.ServerMessage)
	// Block forever so ticker path runs.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(700 * time.Millisecond)
		_ = w.Close()
	}()

	done := make(chan struct{})
	go func() {
		ss.runFramebufferPollerInner(ctx, ch, true)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("poller did not exit after write error")
	}

	time.Sleep(200 * time.Millisecond) // reconnect runs in a goroutine
	if atomic.LoadInt32(&reconnectCalled) != 1 {
		t.Fatalf("reconnect calls = %d, want 1", atomic.LoadInt32(&reconnectCalled))
	}
	_ = r.Close()
}

func TestRunFramebufferPollerInner_TickerStopsWhenStopped(t *testing.T) {
	r, w := net.Pipe()
	defer r.Close()
	defer w.Close()
	go func() { _, _ = io.Copy(io.Discard, r) }()

	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	wireClientConnForTest(conn, w)
	ss := newVNCViewServer(conn)

	ch := make(chan vnc.ServerMessage)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(50 * time.Millisecond)
		ss.Stop()
	}()

	done := make(chan struct{})
	go func() {
		ss.runFramebufferPollerInner(ctx, ch, true)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("poller did not exit after Stop")
	}
}
