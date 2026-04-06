// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"net"
	"sync"
	"time"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	vnc "github.com/mitchellh/go-vnc"
)

// waitForNewConnDeadline is how long WaitForNewConn waits for a new upstream
// connection; tests may shorten it to avoid sleeping for five minutes.
var waitForNewConnDeadline = 5 * time.Minute

// vncScreenshotter maintains an in-memory RGBA framebuffer updated from
// upstream VNC FramebufferUpdate messages, and presents it as an RFB 3.8
// server on a local TCP port. Any VNC viewer can connect to localhost:<port>
// to watch the build and type into the VM. Key and pointer events are
// forwarded to the upstream Bhyve VNC connection.
type vncViewServer struct {
	conn      *vnc.ClientConn
	mu        sync.RWMutex
	img       *image.RGBA
	updMu     sync.Mutex
	updCh     chan struct{} // closed on each framebuffer update
	connMu    sync.Mutex
	connCh    chan struct{} // closed and replaced when conn changes
	listener  net.Listener  // held open for the view server, closed by Stop()
	stopped   bool          // set by Stop() to prevent auto-reconnect
	reconnect vncReconnectFunc
	ui        packersdk.Ui
	clientsMu sync.Mutex
	clients   map[net.Conn]struct{} // active viewer connections, closed by Stop()
}

// vncReconnectFunc is a function that establishes a new upstream VNC
// connection and resumes the framebuffer poller. It is stored in the
// state bag under "vnc_reconnect" and on the vncViewServer so the
// framebuffer poller can auto-reconnect when the upstream drops (e.g.
// due to a guest reboot mid-boot-command).
type vncReconnectFunc func(ctx context.Context, ui packersdk.Ui) error

func newVNCViewServer(conn *vnc.ClientConn) *vncViewServer {
	w := int(conn.FrameBufferWidth)
	h := int(conn.FrameBufferHeight)
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	return &vncViewServer{
		conn:    conn,
		img:     image.NewRGBA(image.Rect(0, 0, w, h)),
		updCh:   make(chan struct{}),
		connCh:  make(chan struct{}),
		clients: make(map[net.Conn]struct{}),
	}
}

// notifyUpdated signals all waiters that the framebuffer has changed.
func (ss *vncViewServer) notifyUpdated() {
	ss.updMu.Lock()
	old := ss.updCh
	ss.updCh = make(chan struct{})
	ss.updMu.Unlock()
	close(old)
}

// waitUpdated returns a channel that is closed when the next update arrives.
func (ss *vncViewServer) waitUpdated() <-chan struct{} {
	ss.updMu.Lock()
	defer ss.updMu.Unlock()
	return ss.updCh
}

// notifyConnChanged signals waiters that the upstream conn has been replaced.
func (ss *vncViewServer) notifyConnChanged() {
	ss.connMu.Lock()
	old := ss.connCh
	ss.connCh = make(chan struct{})
	ss.connMu.Unlock()
	close(old)
}

// waitConnChanged returns a channel closed when the conn next changes.
func (ss *vncViewServer) waitConnChanged() <-chan struct{} {
	ss.connMu.Lock()
	defer ss.connMu.Unlock()
	return ss.connCh
}

// GetConn returns the current upstream VNC connection.
func (ss *vncViewServer) GetConn() *vnc.ClientConn {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.conn
}

// WaitForNewConn blocks until the upstream conn differs from old, then returns
// the new conn. Returns an error if ctx is cancelled, the server is stopped,
// or no new conn arrives within waitForNewConnDeadline.
func (ss *vncViewServer) WaitForNewConn(ctx context.Context, old *vnc.ClientConn) (*vnc.ClientConn, error) {
	deadline := time.Now().Add(waitForNewConnDeadline)
	for {
		ss.mu.RLock()
		stopped := ss.stopped
		ss.mu.RUnlock()
		if stopped {
			return nil, fmt.Errorf("VNC connection closed")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("timed out waiting for VNC reconnect after 5 minutes")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ss.waitConnChanged():
		case <-time.After(remaining):
			continue
		}
		if c := ss.GetConn(); c != nil && c != old {
			return c, nil
		}
	}
}

// Stop marks the server as stopped (preventing further auto-reconnects),
// closes the upstream VNC connection, closes the local viewer listener, and
// closes all connected viewer clients so they receive a clean disconnect.
// Call this after boot_command finishes so the Sylve WebUI can reclaim the
// VNC port.
func (ss *vncViewServer) Stop() {
	ss.mu.Lock()
	ss.stopped = true
	conn := ss.conn
	ln := ss.listener
	ss.conn = nil
	ss.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	if ln != nil {
		_ = ln.Close()
	}
	// Close all active viewer connections so their goroutines unblock and exit.
	ss.clientsMu.Lock()
	for c := range ss.clients {
		_ = c.Close()
	}
	ss.clientsMu.Unlock()
	ss.notifyConnChanged() // wake any WaitForNewConn callers
}

// applyUpdate blends a FramebufferUpdateMessage into the in-memory framebuffer.
// Only RawEncoding rectangles are processed; other encodings are ignored.
func (ss *vncViewServer) applyUpdate(msg *vnc.FramebufferUpdateMessage) {
	ss.mu.RLock()
	upConn := ss.conn
	ss.mu.RUnlock()
	if upConn == nil {
		return
	}
	pf := upConn.PixelFormat
	rMax := uint32(pf.RedMax)
	gMax := uint32(pf.GreenMax)
	bMax := uint32(pf.BlueMax)
	if rMax == 0 {
		rMax = 1
	}
	if gMax == 0 {
		gMax = 1
	}
	if bMax == 0 {
		bMax = 1
	}

	ss.mu.Lock()
	for _, rect := range msg.Rectangles {
		raw, ok := rect.Enc.(*vnc.RawEncoding)
		if !ok {
			continue
		}
		for i, c := range raw.Colors {
			px := int(rect.X) + i%int(rect.Width)
			py := int(rect.Y) + i/int(rect.Width)
			ss.img.SetRGBA(px, py, color.RGBA{
				R: uint8(uint32(c.R) * 255 / rMax),
				G: uint8(uint32(c.G) * 255 / gMax),
				B: uint8(uint32(c.B) * 255 / bMax),
				A: 255,
			})
		}
	}
	ss.mu.Unlock()
	ss.notifyUpdated()
}

// start starts the view server using a pre-bound listener obtained from the
// state bag (keyed "vnc_view_listener"). Holding an open listener across the
// VM-creation and VNC-connect steps eliminates the TOCTOU race that would
// exist if we released the port during selection and re-bound it here.
// Stops accepting when ctx is cancelled.
func (ss *vncViewServer) start(ctx context.Context, ln net.Listener) int {
	port := ln.Addr().(*net.TCPAddr).Port
	ss.mu.Lock()
	ss.listener = ln
	ss.mu.Unlock()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go ss.handleVNCClient(c)
		}
	}()
	return port
}

// handleVNCClient performs the RFB 3.8 handshake, serves the framebuffer,
// and forwards key and pointer events to the upstream Bhyve VNC connection.
// Multiple viewers can connect simultaneously.
func (ss *vncViewServer) handleVNCClient(c net.Conn) {
	ss.clientsMu.Lock()
	ss.clients[c] = struct{}{}
	ss.clientsMu.Unlock()
	defer func() {
		ss.clientsMu.Lock()
		delete(ss.clients, c)
		ss.clientsMu.Unlock()
		c.Close()
	}()

	// --- Version handshake ---
	if _, err := c.Write([]byte("RFB 003.008\n")); err != nil {
		return
	}
	verBuf := make([]byte, 12)
	if _, err := io.ReadFull(c, verBuf); err != nil {
		return
	}

	// --- Security: offer only None (type 1) ---
	// [n_types=1] [type=1]
	if _, err := c.Write([]byte{1, 1}); err != nil {
		return
	}
	secChoice := make([]byte, 1)
	if _, err := io.ReadFull(c, secChoice); err != nil {
		return
	}
	// SecurityResult: OK
	if _, err := c.Write([]byte{0, 0, 0, 0}); err != nil {
		return
	}

	// --- ClientInit (shared flag, ignored) ---
	initBuf := make([]byte, 1)
	if _, err := io.ReadFull(c, initBuf); err != nil {
		return
	}

	// --- ServerInit ---
	ss.mu.RLock()
	fw := uint16(ss.img.Bounds().Dx())
	fh := uint16(ss.img.Bounds().Dy())
	ss.mu.RUnlock()

	name := []byte("Sylve VM")
	// PixelFormat (16 bytes): 32bpp, depth 24, little-endian, TrueColor.
	// Pixel layout in memory: [B, G, R, 0] (RedShift=16, GreenShift=8, BlueShift=0).
	pixFmt := [16]byte{
		32,     // bits-per-pixel
		24,     // depth
		0,      // big-endian-flag (0 = little-endian)
		1,      // true-colour-flag
		0, 255, // red-max   (big-endian uint16 = 255)
		0, 255, // green-max
		0, 255, // blue-max
		16, 8, 0, // red-shift, green-shift, blue-shift
		0, 0, 0, // padding
	}
	nl := uint32(len(name))
	serverInit := make([]byte, 0, 24+len(name))
	serverInit = append(serverInit, byte(fw>>8), byte(fw), byte(fh>>8), byte(fh))
	serverInit = append(serverInit, pixFmt[:]...)
	serverInit = append(serverInit, byte(nl>>24), byte(nl>>16), byte(nl>>8), byte(nl))
	serverInit = append(serverInit, name...)
	if _, err := c.Write(serverInit); err != nil {
		return
	}

	// --- Main message loop ---
	for {
		typeBuf := make([]byte, 1)
		if _, err := io.ReadFull(c, typeBuf); err != nil {
			return
		}
		switch typeBuf[0] {
		case 0: // SetPixelFormat: 3 padding + 16 pixel-format bytes
			buf := make([]byte, 19)
			if _, err := io.ReadFull(c, buf); err != nil {
				return
			}

		case 2: // SetEncodings: 1 padding + 2 count + count*4 encoding IDs
			hdr := make([]byte, 3)
			if _, err := io.ReadFull(c, hdr); err != nil {
				return
			}
			count := int(binary.BigEndian.Uint16(hdr[1:3]))
			encs := make([]byte, count*4)
			if _, err := io.ReadFull(c, encs); err != nil {
				return
			}

		case 3: // FramebufferUpdateRequest: incremental(1) x(2) y(2) w(2) h(2)
			buf := make([]byte, 9)
			if _, err := io.ReadFull(c, buf); err != nil {
				return
			}
			incremental := buf[0] != 0
			x := binary.BigEndian.Uint16(buf[1:3])
			y := binary.BigEndian.Uint16(buf[3:5])
			rw := binary.BigEndian.Uint16(buf[5:7])
			rh := binary.BigEndian.Uint16(buf[7:9])
			if incremental {
				// Block until the framebuffer changes or 500 ms passes.
				select {
				case <-ss.waitUpdated():
				case <-time.After(500 * time.Millisecond):
				}
			}
			if err := ss.sendFramebufferUpdate(c, x, y, rw, rh); err != nil {
				return
			}

		case 4: // KeyEvent: down(1) padding(2) keysym(4) — forward to upstream
			buf := make([]byte, 7)
			if _, err := io.ReadFull(c, buf); err != nil {
				return
			}
			down := buf[0] != 0
			keysym := binary.BigEndian.Uint32(buf[3:7])
			if upConn := ss.GetConn(); upConn != nil {
				_ = upConn.KeyEvent(keysym, down)
			}

		case 5: // PointerEvent: button-mask(1) x(2) y(2) — consumed but not forwarded
			buf := make([]byte, 5)
			if _, err := io.ReadFull(c, buf); err != nil {
				return
			}

		case 6: // ClientCutText: padding(3) length(4) text(length)
			hdr := make([]byte, 7)
			if _, err := io.ReadFull(c, hdr); err != nil {
				return
			}
			length := binary.BigEndian.Uint32(hdr[3:7])
			if length > 0 {
				text := make([]byte, length)
				if _, err := io.ReadFull(c, text); err != nil {
					return
				}
			}

		default:
			return
		}
	}
}

// sendFramebufferUpdate writes one FramebufferUpdate covering the requested
// region using Raw encoding with 32bpp little-endian [B, G, R, 0] pixels.
func (ss *vncViewServer) sendFramebufferUpdate(c net.Conn, x, y, w, h uint16) error {
	ss.mu.RLock()
	img := ss.img
	ss.mu.RUnlock()

	bounds := img.Bounds()
	ix, iy, iw, ih := int(x), int(y), int(w), int(h)
	if ix >= bounds.Max.X || iy >= bounds.Max.Y {
		return nil
	}
	if ix+iw > bounds.Max.X {
		iw = bounds.Max.X - ix
	}
	if iy+ih > bounds.Max.Y {
		ih = bounds.Max.Y - iy
	}
	if iw <= 0 || ih <= 0 {
		return nil
	}

	// FramebufferUpdate header: type(1)=0, padding(1)=0, n-rects(2)=1
	hdr := [4]byte{0, 0, 0, 1}
	if _, err := c.Write(hdr[:]); err != nil {
		return err
	}
	// Rectangle header: x(2) y(2) w(2) h(2) encoding(4)=0 (Raw)
	rectHdr := [12]byte{
		byte(ix >> 8), byte(ix),
		byte(iy >> 8), byte(iy),
		byte(iw >> 8), byte(iw),
		byte(ih >> 8), byte(ih),
		0, 0, 0, 0,
	}
	if _, err := c.Write(rectHdr[:]); err != nil {
		return err
	}
	// Pixel data: 32bpp little-endian [B, G, R, 0]
	pixelData := make([]byte, iw*ih*4)
	for row := 0; row < ih; row++ {
		for col := 0; col < iw; col++ {
			p := img.RGBAAt(ix+col, iy+row)
			i := (row*iw + col) * 4
			pixelData[i+0] = p.B
			pixelData[i+1] = p.G
			pixelData[i+2] = p.R
			pixelData[i+3] = 0
		}
	}
	_, err := c.Write(pixelData)
	return err
}

// swapConn replaces the upstream VNC connection and starts a new framebuffer
// poller goroutine. The old connection is closed. The local TCP viewer
// connections remain active; they will see a full-frame update on their next
// FramebufferUpdateRequest.
func (ss *vncViewServer) swapConn(ctx context.Context, newConn *vnc.ClientConn, serverMsgCh <-chan vnc.ServerMessage) {
	ss.mu.Lock()
	if ss.conn != nil {
		_ = ss.conn.Close()
	}
	ss.conn = newConn
	// Resize framebuffer if the new VM reports different dimensions.
	w := int(newConn.FrameBufferWidth)
	h := int(newConn.FrameBufferHeight)
	if w > 0 && h > 0 {
		ss.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	ss.mu.Unlock()
	ss.notifyUpdated()
	ss.notifyConnChanged()
	// New poller after swapConn also gets autoReconnect=true so that a
	// subsequent guest reboot is detected and triggers another reconnect.
	// The old poller already exited after firing one reconnect goroutine,
	// so there is no chain — only one active poller goroutine at any time.
	go ss.runFramebufferPollerInner(ctx, serverMsgCh, true)
}

// runFramebufferPoller is the public entry point that enables auto-reconnect.
func (ss *vncViewServer) runFramebufferPoller(ctx context.Context, serverMsgCh <-chan vnc.ServerMessage) {
	ss.runFramebufferPollerInner(ctx, serverMsgCh, true)
}

// runFramebufferPollerInner is the shared implementation. When autoReconnect
// is true and the upstream channel closes (guest reboot), it calls the stored
// reconnect function in a goroutine so the viewer stays live.
func (ss *vncViewServer) runFramebufferPollerInner(ctx context.Context, serverMsgCh <-chan vnc.ServerMessage, autoReconnect bool) {
	ss.mu.RLock()
	conn := ss.conn
	stopped := ss.stopped
	ss.mu.RUnlock()
	if stopped || conn == nil {
		return
	}
	w := conn.FrameBufferWidth
	h := conn.FrameBufferHeight

	// Request a full frame immediately so the view server is not blank on connect.
	_ = conn.FramebufferUpdateRequest(false, 0, 0, w, h)

	// Periodic ticker drives the update cycle. Using only the ticker (and NOT
	// re-requesting on every received update) caps the FBU request rate at
	// 2/second. Sending a FramebufferUpdateRequest on every received message
	// creates a tight loop that fires 20-30 times/second during active terminal
	// output. At those rates the write mutex competes directly with the
	// key-down/key-up gap (100-200 ms) and causes dropped keystrokes.
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Heartbeat: the periodic write also detects a dead upstream.
			// If bhyve exited without sending a clean WebSocket close (e.g.
			// abrupt process kill), serverMsgCh may not close immediately.
			// A write error is the fastest reliable signal.
			// Use GetConn() under RLock to avoid a nil dereference when
			// Stop() races with the ticker (Stop sets ss.conn = nil).
			ss.mu.RLock()
			tickConn := ss.conn
			stopped := ss.stopped
			ss.mu.RUnlock()
			if stopped || tickConn == nil {
				return
			}
			if err := tickConn.FramebufferUpdateRequest(true, 0, 0, w, h); err != nil {
				if autoReconnect && !stopped && ss.reconnect != nil && ss.ui != nil {
					go func() { _ = ss.reconnect(ctx, ss.ui) }()
				}
				return
			}
		case msg, ok := <-serverMsgCh:
			if !ok {
				// Upstream dropped — guest reboot or bhyve exit.
				ss.mu.RLock()
				stopped := ss.stopped
				ss.mu.RUnlock()
				if autoReconnect && !stopped && ss.reconnect != nil && ss.ui != nil {
					go func() { _ = ss.reconnect(ctx, ss.ui) }()
				}
				return
			}
			if update, ok := msg.(*vnc.FramebufferUpdateMessage); ok {
				ss.applyUpdate(update)
				// Do NOT re-request here; the ticker handles the next request.
			}
		}
	}
}
