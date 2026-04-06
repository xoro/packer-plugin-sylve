// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	vnc "github.com/mitchellh/go-vnc"
)

func TestHandleVNCClient_SendsRFBVersionBanner(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	ss := newVNCViewServer(conn)
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
	buf := make([]byte, 12)
	if _, err := io.ReadFull(cli, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "RFB 003.008\n" {
		t.Fatalf("RFB banner = %q, want RFB 003.008\\n", buf)
	}
}

func TestNewVNCViewServer_DefaultsToOneByOneWhenNonPositive(t *testing.T) {
	conn := &vnc.ClientConn{
		FrameBufferWidth:  0,
		FrameBufferHeight: 0,
	}
	ss := newVNCViewServer(conn)
	b := ss.img.Bounds()
	if b.Dx() != 1 || b.Dy() != 1 {
		t.Fatalf("expected 1x1 framebuffer, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestNewVNCViewServer_PreservesDimensions(t *testing.T) {
	conn := &vnc.ClientConn{
		FrameBufferWidth:  64,
		FrameBufferHeight: 48,
	}
	ss := newVNCViewServer(conn)
	b := ss.img.Bounds()
	if b.Dx() != 64 || b.Dy() != 48 {
		t.Fatalf("expected 64x48 framebuffer, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestVNCViewServer_NotifyUpdatedClosesWaitChannel(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	ss := newVNCViewServer(conn)
	ch := ss.waitUpdated()
	done := make(chan struct{})
	go func() {
		<-ch
		close(done)
	}()
	ss.notifyUpdated()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitUpdated channel was not closed after notifyUpdated")
	}
}

func TestVNCViewServer_GetConn(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	if ss.GetConn() != conn {
		t.Fatal("GetConn returned unexpected connection")
	}
}

func TestVNCViewServer_NotifyConnChangedClosesWaitChannel(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	ch := ss.waitConnChanged()
	done := make(chan struct{})
	go func() {
		<-ch
		close(done)
	}()
	ss.notifyConnChanged()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitConnChanged channel was not closed after notifyConnChanged")
	}
}

func TestVNCViewServer_WaitForNewConn_ReturnsWhenConnSwapped(t *testing.T) {
	oldConn := &vnc.ClientConn{FrameBufferWidth: 1, FrameBufferHeight: 1}
	newConn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(oldConn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		ss.mu.Lock()
		ss.conn = newConn
		ss.mu.Unlock()
		ss.notifyConnChanged()
	}()
	got, err := ss.WaitForNewConn(ctx, oldConn)
	wg.Wait()
	if err != nil {
		t.Fatalf("WaitForNewConn: %v", err)
	}
	if got != newConn {
		t.Fatal("expected swapped connection")
	}
}

func TestVNCViewServer_WaitForNewConn_ErrorWhenStopped(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	ss.conn = nil
	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		ss.Stop()
	}()
	_, err := ss.WaitForNewConn(ctx, nil)
	wg.Wait()
	if err == nil {
		t.Fatal("expected error after Stop")
	}
}

func TestVNCViewServer_Stop_WithoutPanic(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	ss.conn = nil
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ss.listener = ln
	r, w := net.Pipe()
	ss.clientsMu.Lock()
	ss.clients[w] = struct{}{}
	ss.clientsMu.Unlock()
	go func() { _, _ = io.Copy(io.Discard, r) }()
	ss.Stop()
	if ss.GetConn() != nil {
		t.Fatal("conn should be nil after Stop")
	}
}

func TestApplyUpdate_NoUpstreamConn(t *testing.T) {
	conn := &vnc.ClientConn{
		FrameBufferWidth:  2,
		FrameBufferHeight: 2,
		PixelFormat: vnc.PixelFormat{
			BPP:       32,
			TrueColor: true,
			RedMax:    255, GreenMax: 255, BlueMax: 255,
		},
	}
	ss := newVNCViewServer(conn)
	ss.conn = nil
	ss.applyUpdate(&vnc.FramebufferUpdateMessage{
		Rectangles: []vnc.Rectangle{{
			X: 0, Y: 0, Width: 1, Height: 1,
			Enc: &vnc.RawEncoding{Colors: []vnc.Color{{R: 128, G: 64, B: 32}}},
		}},
	})
}

func TestApplyUpdate_ZeroColorMaxUsesOne(t *testing.T) {
	conn := &vnc.ClientConn{
		FrameBufferWidth:  2,
		FrameBufferHeight: 2,
		PixelFormat: vnc.PixelFormat{
			BPP:       32,
			TrueColor: true,
			RedMax:    0, GreenMax: 0, BlueMax: 0,
		},
	}
	ss := newVNCViewServer(conn)
	ss.applyUpdate(&vnc.FramebufferUpdateMessage{
		Rectangles: []vnc.Rectangle{{
			X: 0, Y: 0, Width: 1, Height: 1,
			Enc: &vnc.RawEncoding{Colors: []vnc.Color{{R: 1, G: 1, B: 1}}},
		}},
	})
}

func TestApplyUpdate_RawRectangle(t *testing.T) {
	conn := &vnc.ClientConn{
		FrameBufferWidth:  2,
		FrameBufferHeight: 2,
		PixelFormat: vnc.PixelFormat{
			BPP:       32,
			TrueColor: true,
			RedMax:    255, GreenMax: 255, BlueMax: 255,
		},
	}
	ss := newVNCViewServer(conn)
	ss.applyUpdate(&vnc.FramebufferUpdateMessage{
		Rectangles: []vnc.Rectangle{{
			X: 0, Y: 0, Width: 1, Height: 1,
			Enc: &vnc.RawEncoding{Colors: []vnc.Color{{R: 255, G: 128, B: 64}}},
		}},
	})
	c := ss.img.RGBAAt(0, 0)
	if c.R == 0 && c.G == 0 && c.B == 0 {
		t.Fatal("expected non-zero pixel after applyUpdate")
	}
}

func TestApplyUpdate_SkipsNonRawEncoding(t *testing.T) {
	conn := &vnc.ClientConn{
		FrameBufferWidth:  2,
		FrameBufferHeight: 2,
		PixelFormat: vnc.PixelFormat{
			BPP:       32,
			TrueColor: true,
			RedMax:    255, GreenMax: 255, BlueMax: 255,
		},
	}
	ss := newVNCViewServer(conn)
	before := ss.img.RGBAAt(0, 0)
	ss.applyUpdate(&vnc.FramebufferUpdateMessage{
		Rectangles: []vnc.Rectangle{{
			X: 0, Y: 0, Width: 1, Height: 1,
			Enc: fakeNonRawEncoding{},
		}},
	})
	after := ss.img.RGBAAt(0, 0)
	if before != after {
		t.Fatal("non-raw encoding should not modify framebuffer")
	}
}

type fakeNonRawEncoding struct{}

func (fakeNonRawEncoding) Type() int32 { return 99 }

func (fakeNonRawEncoding) Read(*vnc.ClientConn, *vnc.Rectangle, io.Reader) (vnc.Encoding, error) {
	return nil, nil
}

func TestSendFramebufferUpdate_OutOfBoundsNoOp(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	defer r.Close()
	go func() { _, _ = io.Copy(io.Discard, r) }()
	defer w.Close()
	// Region entirely past framebuffer — no writes to w
	if err := ss.sendFramebufferUpdate(w, 10, 10, 1, 1); err != nil {
		t.Fatalf("sendFramebufferUpdate: %v", err)
	}
}

func TestSendFramebufferUpdate_WritesPixels(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	var written int64
	done := make(chan struct{})
	go func() {
		written, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	if err := ss.sendFramebufferUpdate(w, 0, 0, 2, 2); err != nil {
		_ = w.Close()
		<-done
		t.Fatalf("sendFramebufferUpdate: %v", err)
	}
	_ = w.Close()
	<-done
	if written < 20 {
		t.Fatalf("expected framebuffer update payload, got %d bytes", written)
	}
}

func TestSendFramebufferUpdate_ZeroSizeRegion(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	defer r.Close()
	go func() { _, _ = io.Copy(io.Discard, r) }()
	defer w.Close()
	if err := ss.sendFramebufferUpdate(w, 0, 0, 0, 0); err != nil {
		t.Fatalf("sendFramebufferUpdate: %v", err)
	}
}

func TestSendFramebufferUpdate_WriteError(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	_ = r.Close()
	err := ss.sendFramebufferUpdate(w, 0, 0, 2, 2)
	_ = w.Close()
	if err == nil {
		t.Fatal("expected error when connection is broken")
	}
}

func TestSendFramebufferUpdate_ClipsToFramebuffer(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 3, FrameBufferHeight: 3}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	var written int64
	done := make(chan struct{})
	go func() {
		written, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	// Request wider than image — should clip to remaining columns
	if err := ss.sendFramebufferUpdate(w, 1, 1, 10, 2); err != nil {
		_ = w.Close()
		<-done
		t.Fatalf("sendFramebufferUpdate: %v", err)
	}
	_ = w.Close()
	<-done
	if written < 20 {
		t.Fatalf("expected clipped update bytes, got %d", written)
	}
}

func TestSendFramebufferUpdate_ClipsHeight(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 3, FrameBufferHeight: 3}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	var written int64
	done := make(chan struct{})
	go func() {
		written, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	// Request taller than remaining rows — should clip ih to bounds.Max.Y - iy
	if err := ss.sendFramebufferUpdate(w, 0, 1, 3, 10); err != nil {
		_ = w.Close()
		<-done
		t.Fatalf("sendFramebufferUpdate: %v", err)
	}
	_ = w.Close()
	<-done
	if written < 20 {
		t.Fatalf("expected clipped update bytes, got %d", written)
	}
}

func TestSendFramebufferUpdate_ZeroWidthReturnsWithoutWrite(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	var written int64
	done := make(chan struct{})
	go func() {
		written, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	if err := ss.sendFramebufferUpdate(w, 0, 0, 0, 4); err != nil {
		_ = w.Close()
		<-done
		t.Fatalf("sendFramebufferUpdate: %v", err)
	}
	_ = w.Close()
	<-done
	if written != 0 {
		t.Fatalf("expected no framebuffer bytes for zero width, got %d", written)
	}
}

// errAfterNWrites fails Write after maxOK successful writes (used to force an
// error on the pixel-data Write in sendFramebufferUpdate).
type errAfterNWrites struct {
	net.Conn
	n     int
	maxOK int
}

func (e *errAfterNWrites) Write(b []byte) (int, error) {
	e.n++
	if e.n > e.maxOK {
		return 0, fmt.Errorf("simulated write failure")
	}
	return e.Conn.Write(b)
}

func TestSendFramebufferUpdate_PixelDataWriteError(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	go func() {
		_, _ = io.Copy(io.Discard, r)
	}()
	ew := &errAfterNWrites{Conn: w, maxOK: 2}
	err := ss.sendFramebufferUpdate(ew, 0, 0, 2, 2)
	if err == nil {
		_ = w.Close()
		t.Fatal("expected error from pixel data Write")
	}
	_ = w.Close()
}

func TestSendFramebufferUpdate_HeaderWriteError(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	go func() {
		_, _ = io.Copy(io.Discard, r)
	}()
	ew := &errAfterNWrites{Conn: w, maxOK: 0}
	err := ss.sendFramebufferUpdate(ew, 0, 0, 2, 2)
	if err == nil {
		_ = w.Close()
		t.Fatal("expected error from framebuffer header Write")
	}
	_ = w.Close()
}

func TestSendFramebufferUpdate_RectHeaderWriteError(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	go func() {
		_, _ = io.Copy(io.Discard, r)
	}()
	ew := &errAfterNWrites{Conn: w, maxOK: 1}
	err := ss.sendFramebufferUpdate(ew, 0, 0, 2, 2)
	if err == nil {
		_ = w.Close()
		t.Fatal("expected error from rectangle header Write")
	}
	_ = w.Close()
}

func TestSendFramebufferUpdate_ZeroHeightReturnsWithoutWrite(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	ss := newVNCViewServer(conn)
	r, w := net.Pipe()
	var written int64
	done := make(chan struct{})
	go func() {
		written, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	if err := ss.sendFramebufferUpdate(w, 0, 0, 4, 0); err != nil {
		_ = w.Close()
		<-done
		t.Fatalf("sendFramebufferUpdate: %v", err)
	}
	_ = w.Close()
	<-done
	if written != 0 {
		t.Fatalf("expected no framebuffer bytes for zero height, got %d", written)
	}
}

func TestVNCViewServer_WaitForNewConn_ContextCancelled(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ss.WaitForNewConn(ctx, nil)
	if err != context.Canceled {
		t.Fatalf("WaitForNewConn: got %v, want context.Canceled", err)
	}
}

func TestVNCViewServer_WaitForNewConn_TimesOutWhenNoNewConn(t *testing.T) {
	old := waitForNewConnDeadline
	waitForNewConnDeadline = 50 * time.Millisecond
	t.Cleanup(func() { waitForNewConnDeadline = old })

	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	_, err := ss.WaitForNewConn(context.Background(), conn)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVNCViewServer_WaitForNewConn_StopsWhenStopped(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := ss.WaitForNewConn(ctx, conn)
	if err == nil {
		t.Fatal("expected error when server stopped or timeout")
	}
}

func TestVNCViewServer_StartReturnsPort(t *testing.T) {
	conn := &vnc.ClientConn{FrameBufferWidth: 2, FrameBufferHeight: 2}
	ss := newVNCViewServer(conn)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port := ss.start(ctx, ln)
	if port <= 0 {
		t.Fatalf("expected positive port, got %d", port)
	}
}
