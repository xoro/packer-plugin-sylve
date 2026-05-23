// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packer "github.com/hashicorp/packer-plugin-sdk/packer"
	vnc "github.com/mitchellh/go-vnc"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func TestSylveHostIsLocal_InterfaceAddrsError_ISO(t *testing.T) {
	orig := interfaceAddrsFn
	t.Cleanup(func() { interfaceAddrsFn = orig })
	interfaceAddrsFn = func() ([]net.Addr, error) {
		return nil, errors.New("no interfaces")
	}
	if sylveHostIsLocal("192.0.2.50") {
		t.Fatal("expected false")
	}
}

func TestSylveHostIsLocal_IPAddrTypeBranch_ISO(t *testing.T) {
	orig := interfaceAddrsFn
	t.Cleanup(func() { interfaceAddrsFn = orig })
	interfaceAddrsFn = func() ([]net.Addr, error) {
		return []net.Addr{&net.IPAddr{IP: net.ParseIP("192.0.2.77")}}, nil
	}
	if !sylveHostIsLocal("192.0.2.77") {
		t.Fatal("expected local")
	}
}

func TestSshConfigForHost_UserHomeDirError_ISO(t *testing.T) {
	orig := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = orig })
	userHomeDirFn = func() (string, error) {
		return "", errors.New("no home")
	}
	if u, k, p := sshConfigForHost("host"); u != "" || k != "" || p != "" {
		t.Fatalf("got user=%q key=%q jump=%q", u, k, p)
	}
}

func TestExclusiveListen_RawConnControlError(t *testing.T) {
	orig := rawConnControlFn
	t.Cleanup(func() { rawConnControlFn = orig })
	rawConnControlFn = func(_ syscall.RawConn, _ func(uintptr)) error {
		return errors.New("control failed")
	}
	if err := exclusiveListenFn("127.0.0.1:0"); err == nil {
		t.Fatal("expected control error")
	}
}

func TestExclusiveListen_SetSockOptError(t *testing.T) {
	origSet := setSOReuseAddrZeroFn
	t.Cleanup(func() { setSOReuseAddrZeroFn = origSet })
	setSOReuseAddrZeroFn = func(_ uintptr) error {
		return errors.New("setsockopt failed")
	}
	if err := exclusiveListenFn("127.0.0.1:0"); err == nil {
		t.Fatal("expected setsockopt error")
	}
}

func TestStepCreateVM_selectVNCPort_ListenErrorThenNextPort(t *testing.T) {
	origListen := vncPortListenFn
	origExclusive := exclusiveListenFn
	origRemote := isRemoteHostForVNCPort
	t.Cleanup(func() {
		vncPortListenFn = origListen
		exclusiveListenFn = origExclusive
		isRemoteHostForVNCPort = origRemote
	})
	isRemoteHostForVNCPort = func(string) bool { return false }
	exclusiveListenFn = func(string) error { return nil }

	var listenN int
	vncPortListenFn = func(_, addr string) (net.Listener, error) {
		listenN++
		if strings.HasSuffix(addr, ":5900") {
			return nil, errors.New("port busy")
		}
		return net.Listen("tcp", addr)
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/vm/simple" {
			http.NotFound(w, r)
			return
		}
		resp := client.APIResponse[[]client.SimpleVM]{Status: "success", Data: nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok", true)
	state := new(multistep.BasicStateBag)
	step := &StepCreateVM{Config: &Config{VNCPortMin: 5900, VNCPortMax: 5901, VNCHost: "127.0.0.1"}}
	if err := step.selectVNCPort(c, state); err != nil {
		t.Fatalf("selectVNCPort: %v", err)
	}
	if step.Config.VNCPort != 5901 {
		t.Fatalf("VNCPort=%d want 5901", step.Config.VNCPort)
	}
	if listenN < 2 {
		t.Fatalf("listenN=%d", listenN)
	}
}

func TestStepDownloadISO_PollLoopFindErrorContinues(t *testing.T) {
	restoreDownloadISODurations(t)
	const isoURL = "https://example.com/poll-err.iso"
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			if n == 1 {
				_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: nil})
				return
			}
			if n == 2 {
				http.Error(w, "db down", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{
				Status: "ok",
				Data: []client.Download{{
					URL: isoURL, Status: client.DownloadStatusDone, UUID: "done-uuid",
				}},
			})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepDownloadISO{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true, ISODownloadURL: isoURL,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
}

func TestLocalHTTPIPForSwitch_InterfaceAddrsError(t *testing.T) {
	orig := netInterfaces
	t.Cleanup(func() { netInterfaces = orig })
	netInterfaces = func() ([]net.Interface, error) {
		return nil, errors.New("ifaces failed")
	}
	got := localHTTPIPForSwitch("127.0.0.1", "127.0.0.1")
	if got != "127.0.0.1" {
		t.Fatalf("got %q", got)
	}
}

func TestLocalHTTPIPForSwitch_IPAddrBranch(t *testing.T) {
	orig := netInterfaces
	t.Cleanup(func() { netInterfaces = orig })
	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Flags: net.FlagUp}}, nil
	}
	origAddrs := ifaceAddrsForLocalHTTPIP
	ifaceAddrsForLocalHTTPIP = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{&net.IPAddr{IP: net.ParseIP("10.250.0.5")}}, nil
	}
	t.Cleanup(func() { ifaceAddrsForLocalHTTPIP = origAddrs })

	got := localHTTPIPForSwitch("127.0.0.1", "does.not.resolve.invalid")
	if got != "10.250.0.5" {
		t.Fatalf("got %q", got)
	}
}

func TestLocalHTTPIPForSwitch_SkipsSylveHostIPs(t *testing.T) {
	orig := netInterfaces
	t.Cleanup(func() { netInterfaces = orig })
	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Flags: net.FlagUp}}, nil
	}
	origAddrs := ifaceAddrsForLocalHTTPIP
	ifaceAddrsForLocalHTTPIP = func(net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("10.200.0.1"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("10.200.0.2"), Mask: net.CIDRMask(24, 32)},
		}, nil
	}
	t.Cleanup(func() { ifaceAddrsForLocalHTTPIP = origAddrs })

	got := localHTTPIPForSwitch("127.0.0.1", "10.200.0.1")
	if got != "10.200.0.2" {
		t.Fatalf("got %q want 10.200.0.2", got)
	}
}

// failAfterWriteConn fails Write after n successful bytes.
type failAfterWriteConn struct {
	net.Conn
	n       int
	written int
}

func (c *failAfterWriteConn) Write(b []byte) (int, error) {
	if c.written >= c.n {
		return 0, io.ErrClosedPipe
	}
	n, err := c.Conn.Write(b)
	c.written += n
	return n, err
}

func TestHandleVNCClient_HandshakeWriteErrors(t *testing.T) {
	cases := []struct {
		name      string
		failAfter int
		setup     func(net.Conn) error
	}{
		{
			name:      "security offer",
			failAfter: 12,
			setup: func(r net.Conn) error {
				ver := make([]byte, 12)
				if _, err := io.ReadFull(r, ver); err != nil {
					return err
				}
				if _, err := r.Write([]byte("RFB 003.008\n")); err != nil {
					return err
				}
				return r.Close()
			},
		},
		{
			name:      "security result",
			failAfter: 14,
			setup: func(r net.Conn) error {
				ver := make([]byte, 12)
				if _, err := io.ReadFull(r, ver); err != nil {
					return err
				}
				if _, err := r.Write([]byte("RFB 003.008\n")); err != nil {
					return err
				}
				sec := make([]byte, 2)
				if _, err := io.ReadFull(r, sec); err != nil {
					return err
				}
				if _, err := r.Write([]byte{1}); err != nil {
					return err
				}
				return r.Close()
			},
		},
		{
			name:      "client init read",
			failAfter: 18,
			setup: func(r net.Conn) error {
				ver := make([]byte, 12)
				if _, err := io.ReadFull(r, ver); err != nil {
					return err
				}
				if _, err := r.Write([]byte("RFB 003.008\n")); err != nil {
					return err
				}
				sec := make([]byte, 2)
				if _, err := io.ReadFull(r, sec); err != nil {
					return err
				}
				if _, err := r.Write([]byte{1}); err != nil {
					return err
				}
				res := make([]byte, 4)
				if _, err := io.ReadFull(r, res); err != nil {
					return err
				}
				return r.Close()
			},
		},
		{
			name:      "server init",
			failAfter: 18,
			setup: func(r net.Conn) error {
				ver := make([]byte, 12)
				if _, err := io.ReadFull(r, ver); err != nil {
					return err
				}
				if _, err := r.Write([]byte("RFB 003.008\n")); err != nil {
					return err
				}
				sec := make([]byte, 2)
				if _, err := io.ReadFull(r, sec); err != nil {
					return err
				}
				if _, err := r.Write([]byte{1}); err != nil {
					return err
				}
				if _, err := io.ReadFull(r, make([]byte, 4)); err != nil {
					return err
				}
				if _, err := r.Write([]byte{0}); err != nil {
					return err
				}
				return r.Close()
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w := net.Pipe()
			var wrapped net.Conn = &failAfterWriteConn{Conn: w, n: tc.failAfter}
			if tc.name == "server init" {
				wrapped = &errAfterNWrites{Conn: w, maxOK: 3}
			}
			ss := newVNCViewServer(&vnc.ClientConn{FrameBufferWidth: 4, FrameBufferHeight: 4})
			done := make(chan struct{})
			go func() {
				ss.handleVNCClient(wrapped)
				close(done)
			}()
			if err := tc.setup(r); err != nil && !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("handler hung")
			}
		})
	}
}

func TestHandleVNCClient_IncrementalFBUTimeoutBranch(t *testing.T) {
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

	fbuInc := make([]byte, 10)
	fbuInc[0] = 3
	fbuInc[1] = 1
	binary.BigEndian.PutUint16(fbuInc[6:8], 4)
	binary.BigEndian.PutUint16(fbuInc[8:10], 4)
	if _, err := cli.Write(fbuInc); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	_ = cli.Close()
}

func TestLocalHTTPIPForSwitch_PerInterfaceAddrsError(t *testing.T) {
	orig := netInterfaces
	t.Cleanup(func() { netInterfaces = orig })
	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{{Flags: net.FlagUp}}, nil
	}
	origAddrs := ifaceAddrsForLocalHTTPIP
	ifaceAddrsForLocalHTTPIP = func(net.Interface) ([]net.Addr, error) {
		return nil, errors.New("addrs failed")
	}
	t.Cleanup(func() { ifaceAddrsForLocalHTTPIP = origAddrs })

	got := localHTTPIPForSwitch("127.0.0.1", "127.0.0.1")
	if got != "127.0.0.1" {
		t.Fatalf("got %q", got)
	}
}

func TestStepVNCBootCommand_ReconnectDialHTTPStatusLogged(t *testing.T) {
	orig := vncReconnectRetryDelay
	vncReconnectRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { vncReconnectRetryDelay = orig })

	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
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
	var dialN int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/vnc/") {
			http.NotFound(w, r)
			return
		}
		if atomic.AddInt32(&dialN, 1) == 1 {
			wsConn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			tcpConn, err := net.Dial("tcp", rfbLn.Addr().String())
			if err != nil {
				_ = wsConn.Close()
				return
			}
			go bridgeWebSocketTCP(wsConn, tcpConn)
			return
		}
		http.Error(w, "gone", http.StatusGone)
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "t", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns", BootCommand: []string{"<wait1ms>"},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", ln)

	runCtx, runCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer runCancel()
	if step.Run(runCtx, state) != multistep.ActionContinue {
		t.Fatalf("Run err=%v", state.Get("error"))
	}

	rfn := state.Get("vnc_reconnect").(vncReconnectFunc)
	reCtx, reCancel := context.WithCancel(context.Background())
	reCancel()
	if err := rfn(reCtx, newMockUI()); err == nil {
		t.Fatal("expected cancelled reconnect after dial failure logging")
	}
}

func TestStepVNCBootCommand_ReconnectHandshakeContextCancelled(t *testing.T) {
	orig := vncReconnectRetryDelay
	vncReconnectRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { vncReconnectRetryDelay = orig })

	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
	rfbAddr := rfbLn.Addr().String()

	var acceptN int32
	go func() {
		for {
			c, err := rfbLn.Accept()
			if err != nil {
				return
			}
			n := atomic.AddInt32(&acceptN, 1)
			if n == 1 {
				go func(conn net.Conn) {
					defer conn.Close()
					_ = serveMinimalRFB(conn)
				}(c)
				continue
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = conn.Write([]byte("RFB 003.008\n"))
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
		SylveURL: srv.URL, SylveToken: "t", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns", BootCommand: []string{"<wait1ms>"},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", ln)

	runCtx, runCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer runCancel()
	if step.Run(runCtx, state) != multistep.ActionContinue {
		t.Fatalf("Run err=%v", state.Get("error"))
	}

	rfn := state.Get("vnc_reconnect").(vncReconnectFunc)
	reCtx, reCancel := context.WithCancel(context.Background())
	reCancel()
	if err := rfn(reCtx, newMockUI()); err == nil {
		t.Fatal("expected handshake cancel error")
	}
}

func TestStepVNCBootCommand_BootCommandRetryAfterReconnect(t *testing.T) {
	orig := vncReconnectRetryDelay
	vncReconnectRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { vncReconnectRetryDelay = orig })

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

	viewLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer viewLn.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "test-token", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns",
		BootCommand:     []string{"bbbbbbbb"},
		BootKeyInterval: 50 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", viewLn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		var ss *vncViewServer
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if v, ok := state.Get("vnc_view_server").(*vncViewServer); ok {
				ss = v
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if ss == nil {
			return
		}
		// Drop upstream mid-command; reconnect only after WaitForNewConn is waiting
		// so notifyConnChanged is not missed (see vncViewServer.WaitForNewConn).
		time.Sleep(80 * time.Millisecond)
		ss.mu.Lock()
		if ss.conn != nil {
			_ = ss.conn.Close()
		}
		ss.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		if rfn, ok := state.GetOk("vnc_reconnect"); ok {
			_ = rfn.(vncReconnectFunc)(context.Background(), newMockUI())
		}
	}()

	if step.Run(ctx, state) != multistep.ActionContinue {
		t.Fatalf("Run err=%v", state.Get("error"))
	}
}

func TestSshConfigForHost_ShortLineIgnored_ISO(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ssh", "config"), []byte("Host h\n  User\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if u, _, _ := sshConfigForHost("h"); u != "" {
		t.Fatalf("user=%q", u)
	}
}

// Exercise no-op Cleanup methods registered on multistep steps so defer paths
// and explicit cleanup calls contribute to aggregate coverage totals.
func TestSylveISO_NoOpStepCleanups(t *testing.T) {
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	cfg := &Config{SylveURL: "http://localhost:8181", SylveToken: "t"}

	(&StepDeleteVM{Config: cfg}).Cleanup(state)
	(&StepDiscoverIP{Config: cfg}).Cleanup(state)
	(&StepDownloadISO{Config: cfg}).Cleanup(state)
	(&StepRestartAfterInstall{Config: cfg}).Cleanup(state)
	(&StepShutdown{Config: cfg}).Cleanup(state)
	(&StepStartVM{Config: cfg}).Cleanup(state)
	(&StepVNCBootCommand{Config: cfg}).Cleanup(state)
}

func TestStepVNCBootCommand_InvalidBootWait_Halt(t *testing.T) {
	step := &StepVNCBootCommand{Config: &Config{
		BootWait:      "totally-invalid-duration-string",
		SylveURL:      "https://127.0.0.1:8181",
		SylveToken:    "tok",
		TLSSkipVerify: true,
		VNCPort:       5900,
		VNCHost:       "127.0.0.1",
		BootCommand:   []string{"<wait1ms>"},
		VNCPassword:   "",
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatalf("want halt, got ok err=%v", state.Get("error"))
	}
}

func TestStepVNCBootCommand_InterpolateBootCommandError_Halt(t *testing.T) {
	step := &StepVNCBootCommand{Config: &Config{
		BootWait:      "1ns",
		SylveURL:      "https://127.0.0.1:8181",
		SylveToken:    "tok",
		TLSSkipVerify: true,
		VNCPort:       5900,
		VNCHost:       "127.0.0.1",
		VMName:        "guest",
		// interpolate.Render fails on malformed control sequences.
		BootCommand: []string{"{{ unterminated"},
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatalf("want halt, got ok err=%v", state.Get("error"))
	}
}

func TestStepVNCBootCommand_DerivesHTTPIPWhenMissing(t *testing.T) {
	origDial := netDialUDPForHTTPIP
	t.Cleanup(func() { netDialUDPForHTTPIP = origDial })
	netDialUDPForHTTPIP = func(network, address string) (net.Conn, error) {
		return net.Dial(network, address)
	}

	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
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
		tcpConn, err := net.Dial("tcp", rfbLn.Addr().String())
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	viewLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer viewLn.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "t", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns", BootCommand: []string{"<wait1ms>"},
	}
	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", viewLn)
	state.Put("http_port", 8080)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if step.Run(ctx, state) != multistep.ActionContinue {
		t.Fatalf("Run err=%v", state.Get("error"))
	}
	if ip, ok := state.GetOk("http_ip"); !ok || ip.(string) == "" {
		t.Fatal("expected http_ip to be derived and stored")
	}
}

func TestStepVNCBootCommand_FallbackViewListenerSuccess(t *testing.T) {
	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/vnc/") {
			http.NotFound(w, r)
			return
		}
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		tcpConn, err := net.Dial("tcp", rfbLn.Addr().String())
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "t", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns", BootCommand: []string{"<wait1ms>"},
	}
	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	// Deliberately omit vnc_view_listener to exercise fresh-listener path.

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if step.Run(ctx, state) != multistep.ActionContinue {
		t.Fatalf("Run err=%v", state.Get("error"))
	}
}

func TestStepVNCBootCommand_HTTPSchemeUsesWS(t *testing.T) {
	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/vnc/") {
			http.NotFound(w, r)
			return
		}
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		tcpConn, err := net.Dial("tcp", rfbLn.Addr().String())
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	viewLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer viewLn.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "t", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns", BootCommand: []string{"<wait1ms>"},
	}
	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", viewLn)
	state.Put("http_ip", "127.0.0.1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if step.Run(ctx, state) != multistep.ActionContinue {
		t.Fatalf("Run err=%v", state.Get("error"))
	}
}

func TestStepVNCBootCommand_BootWaitContextCancel(t *testing.T) {
	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
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
		tcpConn, err := net.Dial("tcp", rfbLn.Addr().String())
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	viewLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer viewLn.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "t", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "30s", BootCommand: []string{"<wait1ms>"},
	}
	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", viewLn)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if step.Run(ctx, state) != multistep.ActionHalt {
		t.Fatalf("want halt on boot_wait cancel, err=%v", state.Get("error"))
	}
}

func TestStepVNCBootCommand_BootCommandWaitForNewConnFails(t *testing.T) {
	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
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
		tcpConn, err := net.Dial("tcp", rfbLn.Addr().String())
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	viewLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer viewLn.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "t", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns",
		// Long wait keeps seq.Do blocked; cancel context to force a send error path.
		BootCommand:     []string{"<wait60s>"},
		BootKeyInterval: 1 * time.Millisecond,
	}
	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", viewLn)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go func() {
		time.Sleep(30 * time.Millisecond)
		if ss, ok := state.Get("vnc_view_server").(*vncViewServer); ok {
			ss.Stop()
		}
	}()

	if step.Run(ctx, state) != multistep.ActionHalt {
		t.Fatalf("want halt when boot command cannot complete, err=%v", state.Get("error"))
	}
}

func TestDialTCPForVNCPortProbe_DefaultImpl(t *testing.T) {
	_, err := dialTCPForVNCPortProbe("tcp", "127.0.0.1:1", 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected dial error to unused port")
	}
}

func TestStepVNCBootCommand_ReconnectDialFailureUsesRetryDelay(t *testing.T) {
	orig := vncReconnectRetryDelay
	vncReconnectRetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { vncReconnectRetryDelay = orig })

	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
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
		tcpConn, err := net.Dial("tcp", rfbLn.Addr().String())
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "t", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns", BootCommand: []string{"<wait1ms>"},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", ln)

	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("Run err=%v", state.Get("error"))
	}

	srv.Close()
	rfn := state.Get("vnc_reconnect").(vncReconnectFunc)
	reCtx, reCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer reCancel()
	if err := rfn(reCtx, newMockUI()); err == nil {
		t.Fatal("expected reconnect to fail after dial retries")
	}
}

// cancelOnBootCommandUI cancels the build context when boot commands are about to send.
type cancelOnBootCommandUI struct {
	*packer.MockUi
	cancel context.CancelFunc
}

func (u *cancelOnBootCommandUI) Say(msg string) {
	u.MockUi.Say(msg)
	if strings.Contains(msg, "Sending VNC boot commands") {
		u.cancel()
	}
}

func TestStepVNCBootCommand_BootCommandRetryFailsAfterReconnect(t *testing.T) {
	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
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
		tcpConn, err := net.Dial("tcp", rfbLn.Addr().String())
		if err != nil {
			_ = wsConn.Close()
			return
		}
		go bridgeWebSocketTCP(wsConn, tcpConn)
	}))
	defer srv.Close()

	viewLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer viewLn.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "test-token", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns",
		BootCommand:     []string{"bbbbbbbb"},
		BootKeyInterval: 50 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", viewLn)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	go func() {
		var ss *vncViewServer
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if v, ok := state.Get("vnc_view_server").(*vncViewServer); ok {
				ss = v
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if ss == nil {
			return
		}
		time.Sleep(80 * time.Millisecond)
		ss.mu.Lock()
		if ss.conn != nil {
			_ = ss.conn.Close()
		}
		ss.mu.Unlock()
		time.Sleep(100 * time.Millisecond)
		if rfn, ok := state.GetOk("vnc_reconnect"); ok {
			_ = rfn.(vncReconnectFunc)(context.Background(), newMockUI())
		}
		time.Sleep(50 * time.Millisecond)
		ss.Stop()
	}()

	if step.Run(ctx, state) != multistep.ActionHalt {
		t.Fatalf("want halt after failed retry, err=%v", state.Get("error"))
	}
	if err, ok := state.Get("error").(error); !ok || !strings.Contains(err.Error(), "after reconnect") {
		t.Fatalf("error = %v, want failure after reconnect", state.Get("error"))
	}
}

func TestStepVNCBootCommand_CancelledCtxBeforeBootCommandSend(t *testing.T) {
	rfbLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rfbLn.Close()
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
		tcpConn, err := net.Dial("tcp", rfbLn.Addr().String())
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

	step := &StepVNCBootCommand{Config: nil}
	state := new(multistep.BasicStateBag)
	state.Put("vnc_view_listener", ln)

	ctx, cancel := context.WithCancel(context.Background())
	ui := &cancelOnBootCommandUI{MockUi: newMockUI(), cancel: cancel}
	state.Put("ui", ui)

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "t", VNCPort: 5900, VNCHost: "127.0.0.1",
		TLSSkipVerify: true, BootWait: "1ns",
		BootCommand:     []string{"<wait500ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}
	step.Config = cfg

	if step.Run(ctx, state) != multistep.ActionHalt {
		t.Fatalf("want halt, err=%v", state.Get("error"))
	}
	if err, ok := state.Get("error").(error); !ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", state.Get("error"))
	}
}
