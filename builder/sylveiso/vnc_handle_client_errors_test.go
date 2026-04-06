// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	vnc "github.com/mitchellh/go-vnc"
)

func TestHandleVNCClient_MessageLoopEOF(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_FirstWriteFailsWhenPeerClosed(t *testing.T) {
	r, w := net.Pipe()
	_ = r.Close()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit")
	}
}

func TestHandleVNCClient_PartialClientVersion(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	banner := make([]byte, 12)
	if _, err := io.ReadFull(r, banner); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("RFB")); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

// TestHandleVNCClient_SecurityChoiceReadError closes before sending the 1-byte
// security choice so the server fails reading secChoice.
func TestHandleVNCClient_SecurityChoiceReadError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	banner := make([]byte, 12)
	if _, err := io.ReadFull(r, banner); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	sec := make([]byte, 2)
	if _, err := io.ReadFull(r, sec); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_SetPixelFormatReadError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_KeyEventNilUpstream(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	ss.mu.Lock()
	ss.conn = nil
	ss.mu.Unlock()
	key := make([]byte, 8)
	key[0] = 4
	key[1] = 1
	binary.BigEndian.PutUint32(key[4:8], 0xff)
	if _, err := r.Write(key); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_SetEncodingsHeaderReadError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte{2, 0}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_FBUReadError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte{3, 0}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_SendFBUWriteError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	// Trigger sendFramebufferUpdate then close peer so pixel Write fails mid-flight.
	if _, err := r.Write([]byte{3, 0, 0, 0, 0, 0, 0, 4, 0, 4}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_KeyEventReadError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte{4, 0}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_PointerReadError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte{5, 0, 0}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_SetEncodingsPayloadReadError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	// count=2 encodings => 8 bytes of encoding IDs required; send only 4.
	if _, err := r.Write([]byte{2, 0, 0, 2, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_ClientCutTextHeaderReadError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte{6, 0, 0}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}

func TestHandleVNCClient_ClientCutTextPayloadReadError(t *testing.T) {
	r, w := net.Pipe()
	ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
	done := make(chan struct{})
	go func() {
		ss.handleVNCClient(w)
		close(done)
	}()
	if err := completeRFBHandshake(t, r); err != nil {
		t.Fatal(err)
	}
	// length=4 but only 2 bytes of payload follow
	hdr := []byte{6, 0, 0, 0, 0, 0, 0, 4}
	if _, err := r.Write(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	<-done
}
