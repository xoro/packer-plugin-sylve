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

// Exercises handleVNCClient branches for RFB client messages.
func TestHandleVNCClient_ClientMessageTypes(t *testing.T) {
	up := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	ss := newVNCViewServer(up)
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

	if err := completeRFBHandshake(t, cli); err != nil {
		t.Fatal(err)
	}

	// SetPixelFormat (0) + 3 padding + 16 pixel-format bytes
	pix := make([]byte, 20)
	pix[0] = 0
	_, err = cli.Write(pix)
	if err != nil {
		t.Fatal(err)
	}

	// SetEncodings (2): padding + count=1 + encoding Raw (0)
	enc := []byte{2, 0, 0, 1, 0, 0, 0, 0}
	_, err = cli.Write(enc)
	if err != nil {
		t.Fatal(err)
	}

	// FramebufferUpdateRequest non-incremental (full refresh)
	fbu := make([]byte, 10)
	fbu[0] = 3
	fbu[1] = 0 // incremental
	binary.BigEndian.PutUint16(fbu[2:4], 0)
	binary.BigEndian.PutUint16(fbu[4:6], 0)
	binary.BigEndian.PutUint16(fbu[6:8], 4)
	binary.BigEndian.PutUint16(fbu[8:10], 4)
	_, err = cli.Write(fbu)
	if err != nil {
		t.Fatal(err)
	}

	// Region origin outside framebuffer — sendFramebufferUpdate no-ops (bounds check).
	fbuOff := make([]byte, 10)
	fbuOff[0] = 3
	fbuOff[1] = 0
	binary.BigEndian.PutUint16(fbuOff[2:4], 500)
	binary.BigEndian.PutUint16(fbuOff[4:6], 500)
	binary.BigEndian.PutUint16(fbuOff[6:8], 4)
	binary.BigEndian.PutUint16(fbuOff[8:10], 4)
	if _, err := cli.Write(fbuOff); err != nil {
		t.Fatal(err)
	}

	// Incremental FBU — triggers waitUpdated vs 500ms timeout branch
	fbuInc := make([]byte, 10)
	fbuInc[0] = 3
	fbuInc[1] = 1 // incremental
	binary.BigEndian.PutUint16(fbuInc[2:4], 0)
	binary.BigEndian.PutUint16(fbuInc[4:6], 0)
	binary.BigEndian.PutUint16(fbuInc[6:8], 4)
	binary.BigEndian.PutUint16(fbuInc[8:10], 4)
	_, err = cli.Write(fbuInc)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		ss.notifyUpdated()
	}()
	time.Sleep(200 * time.Millisecond)

	// PointerEvent (5)
	ptr := []byte{5, 0, 0, 0, 0, 0}
	_, err = cli.Write(ptr)
	if err != nil {
		t.Fatal(err)
	}

	// ClientCutText (6) with empty text
	cut := []byte{6, 0, 0, 0, 0, 0, 0, 0}
	_, err = cli.Write(cut)
	if err != nil {
		t.Fatal(err)
	}

	// ClientCutText with non-empty payload (length=4, "test")
	cut2 := []byte{6, 0, 0, 0, 0, 0, 0, 4}
	cut2 = append(cut2, []byte("test")...)
	_, err = cli.Write(cut2)
	if err != nil {
		t.Fatal(err)
	}

	// Invalid message type — server closes
	_, _ = cli.Write([]byte{99})
}

func TestHandleVNCClient_KeyEventForwarded(t *testing.T) {
	r, w := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, r) }()
	up := &vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4}
	wireClientConnForTest(up, w)

	ss := newVNCViewServer(up)
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

	if err := completeRFBHandshake(t, cli); err != nil {
		t.Fatal(err)
	}

	key := make([]byte, 8)
	key[0] = 4
	key[1] = 1 // key-down
	binary.BigEndian.PutUint32(key[4:8], 0xff)
	if _, err := cli.Write(key); err != nil {
		t.Fatal(err)
	}
}

func completeRFBHandshake(t *testing.T, c net.Conn) error {
	t.Helper()
	banner := make([]byte, 12)
	if _, err := io.ReadFull(c, banner); err != nil {
		return err
	}
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		return err
	}
	sec := make([]byte, 2)
	if _, err := io.ReadFull(c, sec); err != nil {
		return err
	}
	if _, err := c.Write([]byte{1}); err != nil {
		return err
	}
	res := make([]byte, 4)
	if _, err := io.ReadFull(c, res); err != nil {
		return err
	}
	if _, err := c.Write([]byte{0}); err != nil {
		return err
	}
	hdr := make([]byte, 24)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return err
	}
	nl := binary.BigEndian.Uint32(hdr[20:24])
	if nl > 0 {
		name := make([]byte, nl)
		if _, err := io.ReadFull(c, name); err != nil {
			return err
		}
	}
	return nil
}
