// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	vnc "github.com/mitchellh/go-vnc"
)

// VNC dial / handshake timings — tests may shorten the overall deadline to hit
// the ActionHalt branches without multi-minute waits.
var (
	vncStepDialRetryDelay  = 3 * time.Second
	vncStepPerConnDeadline = 15 * time.Second
	vncStepOverallDeadline = 3 * time.Minute
	vncReconnectRetryDelay = 500 * time.Millisecond
)

// netDialUDPForHTTPIP is the UDP dial used when deriving http_ip from the local
// route; tests may replace it to exercise the dial-failure path without relying
// on a non-resolvable host.
var netDialUDPForHTTPIP = func(network, address string) (net.Conn, error) {
	return net.Dial(network, address)
}

// localHTTPIPForSwitch returns the IP that the packer HTTP server should
// advertise to the guest via {{ .HTTPIP }} when packer is running on the same
// host as Sylve.
//
// The UDP-dial trick (dialing a UDP socket toward VNCHost) normally selects the
// correct outbound source address. However, when packer runs on the Sylve host
// itself, the dial destination (VNCHost) resolves to one of the machine's own
// addresses. On most OS kernels, dialing to a local address routes through
// loopback and returns 127.0.0.1 — a loopback address that guests on the VM
// bridge cannot reach.
//
// This function detects that case and, when the candidate IP is loopback,
// enumerates all local network interfaces to find the first non-loopback IPv4
// unicast address that does not belong to the Sylve host's own external
// interfaces (identified by resolving vncHost). That address will be the VM
// bridge/switch interface IP (e.g. 10.200.0.1), which the guests can reach.
//
// netInterfaces is a variable so tests can inject a fake interface list.
var netInterfaces = net.Interfaces

func localHTTPIPForSwitch(candidateIP string, vncHost string) string {
	ip := net.ParseIP(candidateIP)
	if ip == nil || !ip.IsLoopback() {
		return candidateIP
	}

	// Collect all IPs that belong to the Sylve host (re0 / external interface).
	sylveHostIPs := map[string]bool{}
	if addrs, err := net.LookupHost(vncHost); err == nil {
		for _, a := range addrs {
			sylveHostIPs[a] = true
		}
	}

	ifaces, err := netInterfaces()
	if err != nil {
		return candidateIP
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ifIP net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ifIP = v.IP
			case *net.IPAddr:
				ifIP = v.IP
			}
			if ifIP == nil || ifIP.IsLoopback() || ifIP.To4() == nil {
				continue
			}
			if sylveHostIPs[ifIP.String()] {
				continue
			}
			return ifIP.String()
		}
	}
	return candidateIP
}

// bootCommandData is the template context for interpolating VNC boot commands.
type bootCommandData struct {
	HTTPIP   string
	HTTPPort int
	Name     string
}

// wsNetConn wraps a gorilla WebSocket connection as a net.Conn so that the
// go-vnc library can use it. Sylve only exposes VNC via a WebSocket proxy
// (Bhyve binds VNC on 127.0.0.1 and is not externally reachable).
type wsNetConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
	buf  []byte
}

// Read fills b from the internal message buffer, reading new WebSocket
// messages from the upstream connection as needed.
func (w *wsNetConn) Read(b []byte) (int, error) {
	for len(w.buf) == 0 {
		_, msg, err := w.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		w.buf = append(w.buf, msg...)
	}
	n := copy(b, w.buf)
	w.buf = w.buf[n:]
	return n, nil
}

// Write sends b as a single WebSocket binary message. The mutex serialises
// concurrent writes from the VNC client and the heartbeat ticker.
func (w *wsNetConn) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (w *wsNetConn) Close() error                       { return w.conn.Close() }
func (w *wsNetConn) LocalAddr() net.Addr                { return w.conn.LocalAddr() }
func (w *wsNetConn) RemoteAddr() net.Addr               { return w.conn.RemoteAddr() }
func (w *wsNetConn) SetDeadline(t time.Time) error      { return w.conn.SetReadDeadline(t) }
func (w *wsNetConn) SetReadDeadline(t time.Time) error  { return w.conn.SetReadDeadline(t) }
func (w *wsNetConn) SetWriteDeadline(t time.Time) error { return w.conn.SetWriteDeadline(t) }

// Ensure wsNetConn satisfies net.Conn at compile time.
var _ net.Conn = (*wsNetConn)(nil)
var _ io.ReadWriter = (*wsNetConn)(nil)

// StepVNCBootCommand connects to the Sylve VM's VNC console via Sylve's
// WebSocket proxy and sends the configured boot_command sequence.
type StepVNCBootCommand struct {
	Config *Config
}

func (s *StepVNCBootCommand) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)

	bootWait, err := s.Config.bootWaitDuration()
	if err != nil {
		state.Put("error", fmt.Errorf("boot_wait: %w", err))
		return multistep.ActionHalt
	}

	httpIP, _ := state.GetOk("http_ip")
	httpPort, _ := state.GetOk("http_port")

	// If http_ip was not set by an earlier step, derive the correct local
	// source address by dialing a UDP socket toward the Sylve host — the
	// kernel selects the right outbound interface without sending any packets.
	// When packer runs on the Sylve host itself the dial returns a loopback
	// address; localHTTPIPForSwitch detects that case and falls back to the
	// VM bridge/switch interface IP instead.
	if httpIP == nil || httpIP.(string) == "" {
		if conn, err := netDialUDPForHTTPIP("udp", net.JoinHostPort(s.Config.VNCHost, "1")); err == nil {
			candidate := conn.LocalAddr().(*net.UDPAddr).IP.String()
			conn.Close()
			httpIP = localHTTPIPForSwitch(candidate, s.Config.VNCHost)
			state.Put("http_ip", httpIP.(string))
		}
	}

	templateData := &bootCommandData{Name: s.Config.VMName}
	if httpIP != nil {
		templateData.HTTPIP = httpIP.(string)
	}
	if httpPort != nil {
		templateData.HTTPPort = httpPort.(int)
	}

	var commands []string
	for i, raw := range s.Config.BootCommand {
		cmd, err := interpolate.Render(raw, &interpolate.Context{Data: templateData})
		if err != nil {
			state.Put("error", fmt.Errorf("boot_command[%d]: %w", i, err))
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		commands = append(commands, cmd)
	}

	// Sylve proxies Bhyve VNC (127.0.0.1-only) via WebSocket at GET /api/vnc/:port.
	// Derive ws(s):// by replacing the scheme in SylveURL (which already contains the port).
	base := strings.Replace(s.Config.SylveURL, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1) // nosemgrep: detect-insecure-websocket — ws:// is only used when the user explicitly configured http:// as the Sylve scheme
	baseURL := strings.TrimSuffix(base, "/")

	// The Sylve EnsureAuthenticated middleware for WebSocket paths (/api/vnc/*)
	// requires a ?auth= query parameter containing hex-encoded JSON with the
	// SHA256 hash of the Bearer token and the server hostname.
	tokenHash := sha256.Sum256([]byte(s.Config.SylveToken))
	wssAuth, _ := json.Marshal(struct {
		Hash     string `json:"hash"`
		Hostname string `json:"hostname"`
	}{
		Hash:     hex.EncodeToString(tokenHash[:]),
		Hostname: s.Config.VNCHost,
	})
	authParam := hex.EncodeToString(wssAuth)
	wsURL := fmt.Sprintf("%s/api/vnc/%d?auth=%s", baseURL, s.Config.VNCPort, authParam)
	log.Printf("[DEBUG] Connecting to VNC via Sylve proxy: %s/api/vnc/%d...", baseURL, s.Config.VNCPort)

	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: s.Config.TLSSkipVerify, // [SECURITY DESIGN] self-signed cert
			MinVersion:         tls.VersionTLS12,       // [SECURITY DESIGN] floor TLS 1.2; Sylve server determines actual version
		},
		HandshakeTimeout: 10 * time.Second,
	}

	wsHeaders := http.Header{}

	// Always include PasswordAuth so the client can negotiate VNC auth type 2
	// (DES challenge-response). Bhyve advertises type 2 even with an empty
	// password; without this handler the handshake fails with "no suitable
	// auth schemes found".
	serverMsgCh := make(chan vnc.ServerMessage, 200)
	vncCfg := &vnc.ClientConfig{
		Exclusive:       false,
		ServerMessageCh: serverMsgCh,
		Auth: []vnc.ClientAuth{
			&vnc.PasswordAuth{Password: s.Config.VNCPassword},
		},
	}

	// Retry both the WebSocket dial and the VNC handshake. Bhyve's VNC listener
	// starts asynchronously; Sylve's proxy will fail (or close the connection
	// immediately) until port VNCPort is actually accepting connections on the
	// server. A 15-second deadline on each handshake attempt prevents blocking
	// indefinitely when the proxy connects but the VNC server is not yet ready.
	vncDeadline := time.Now().Add(vncStepOverallDeadline)
	attempt := 0
	var conn *vnc.ClientConn
	var activeNC *wsNetConn
	for {
		attempt++
		log.Printf("[DEBUG] VNC attempt %d: dialing WebSocket %s...", attempt, wsURL)
		wsConn, wsResp, dialErr := dialer.Dial(wsURL, wsHeaders)
		if dialErr != nil {
			statusCode := 0
			if wsResp != nil {
				statusCode = wsResp.StatusCode
			}
			log.Printf("[DEBUG] VNC attempt %d: WebSocket dial failed (HTTP %d): %s", attempt, statusCode, dialErr)
			if time.Now().After(vncDeadline) {
				err = fmt.Errorf("connect to Sylve VNC proxy %s: %w", wsURL, dialErr)
				state.Put("error", err)
				ui.Error(err.Error())
				return multistep.ActionHalt
			}
			select {
			case <-ctx.Done():
				state.Put("error", ctx.Err())
				return multistep.ActionHalt
			case <-time.After(vncStepDialRetryDelay):
			}
			continue
		}
		log.Printf("[DEBUG] VNC attempt %d: WebSocket connected, starting VNC handshake (15s deadline)...", attempt)

		nc := &wsNetConn{conn: wsConn}
		_ = nc.SetDeadline(time.Now().Add(vncStepPerConnDeadline))
		var handshakeErr error
		conn, handshakeErr = vnc.Client(nc, vncCfg)
		if handshakeErr != nil {
			nc.Close()
			log.Printf("[DEBUG] VNC attempt %d: VNC handshake failed: %s", attempt, handshakeErr)
			if time.Now().After(vncDeadline) {
				err = fmt.Errorf("VNC handshake via Sylve proxy: %w", handshakeErr)
				state.Put("error", err)
				ui.Error(err.Error())
				return multistep.ActionHalt
			}
			select {
			case <-ctx.Done():
				state.Put("error", ctx.Err())
				return multistep.ActionHalt
			case <-time.After(vncStepDialRetryDelay):
			}
			continue
		}
		log.Printf("[DEBUG] VNC attempt %d: VNC handshake succeeded", attempt)
		_ = nc.SetDeadline(time.Time{}) // clear deadline for normal session I/O
		activeNC = nc
		break
	}
	defer activeNC.Close()
	defer conn.Close()

	// Start a local RFB server so the build can be watched (and typed into)
	// with any VNC viewer without consuming the single Bhyve VNC connection
	// the plugin holds for boot commands. The listener was pre-bound during
	// VM creation to avoid a TOCTOU race on the port.
	ss := newVNCViewServer(conn)
	if ln, ok := state.GetOk("vnc_view_listener"); ok {
		vncPort := ss.start(ctx, ln.(net.Listener))
		ui.Say(fmt.Sprintf("VNC view server listening on localhost:%d — connect with any VNC viewer", vncPort))
	} else {
		// The pre-bound listener was released before bhyve started so bhyve could
		// bind its VNC port. Now that the upstream VNC connection is established,
		// bind a fresh listener on any free port for local viewing.
		if freshLN, freshErr := net.Listen("tcp", "127.0.0.1:0"); freshErr == nil {
			vncPort := ss.start(ctx, freshLN)
			ui.Say(fmt.Sprintf("VNC view server listening on localhost:%d — connect with any VNC viewer", vncPort))
		} else {
			ui.Say("VNC view server could not start: no available listener")
		}
	}

	// Build the reconnect closure first so it can be stored on the view server
	// for auto-reconnect when the upstream drops mid-boot-command (guest reboot).
	state.Put("vnc_view_server", ss)
	reconnect := vncReconnectFunc(func(rctx context.Context, rui packersdk.Ui) error {
		log.Printf("[DEBUG] Reconnecting VNC to Sylve proxy: %s/api/vnc/%d...", baseURL, s.Config.VNCPort)
		// Retry every 500ms with no hard deadline: bhyve may not restart for
		// up to several minutes (lifecycle task teardown + startAtBoot delay).
		// The build-level context (rctx) cancels this loop on timeout or Ctrl+C.
		rAttempt := 0
		for {
			rAttempt++
			wsConn, wsResp, dialErr := dialer.Dial(wsURL, http.Header{})
			if dialErr != nil {
				// Log the first attempt and every 20th thereafter to avoid spam
				// while bhyve is still starting up.
				if rAttempt == 1 || rAttempt%20 == 0 {
					statusCode := 0
					if wsResp != nil {
						statusCode = wsResp.StatusCode
					}
					log.Printf("[DEBUG] VNC reconnect attempt %d: dial failed (HTTP %d): %s", rAttempt, statusCode, dialErr)
				}
				select {
				case <-rctx.Done():
					return rctx.Err()
				case <-time.After(vncReconnectRetryDelay):
				}
				continue
			}
			nc := &wsNetConn{conn: wsConn}
			_ = nc.SetDeadline(time.Now().Add(vncStepPerConnDeadline))
			newMsgCh := make(chan vnc.ServerMessage, 200)
			newVNCCfg := &vnc.ClientConfig{
				Exclusive:       false,
				ServerMessageCh: newMsgCh,
				Auth: []vnc.ClientAuth{
					&vnc.PasswordAuth{Password: s.Config.VNCPassword},
				},
			}
			newConn, handshakeErr := vnc.Client(nc, newVNCCfg)
			if handshakeErr != nil {
				nc.Close()
				if rAttempt == 1 || rAttempt%20 == 0 {
					log.Printf("[DEBUG] VNC reconnect attempt %d: handshake failed: %s", rAttempt, handshakeErr)
				}
				select {
				case <-rctx.Done():
					return rctx.Err()
				case <-time.After(vncReconnectRetryDelay):
				}
				continue
			}
			_ = nc.SetDeadline(time.Time{})
			log.Printf("[DEBUG] VNC reconnect attempt %d: connected", rAttempt)
			ss.swapConn(rctx, newConn, newMsgCh)
			return nil
		}
	})
	state.Put("vnc_reconnect", reconnect)
	// Wire reconnect onto the view server so the poller can auto-reconnect
	// when the upstream drops (e.g. guest reboot mid-boot-command sequence).
	ss.reconnect = reconnect
	ss.ui = ui
	go ss.runFramebufferPoller(ctx, serverMsgCh)

	ui.Say(fmt.Sprintf("Waiting %s before sending VNC boot commands...", bootWait))
	select {
	case <-ctx.Done():
		state.Put("error", ctx.Err())
		return multistep.ActionHalt
	case <-time.After(bootWait):
	}

	ui.Say("Sending VNC boot commands...")
	for i, cmd := range commands {
		// Refresh the driver at the start of every iteration. A <waitXs> token
		// in the previous command is a silent sleep — seq.Do returns nil even
		// if bhyve exited mid-wait. The auto-reconnect goroutine may have
		// already completed and swapped the conn by the time we reach the next
		// command, so always use ss.GetConn() rather than stale local variable.
		currentConn := ss.GetConn()
		driver := bootcommand.NewVNCDriver(currentConn, s.Config.BootKeyInterval)

		seq, err := bootcommand.GenerateExpressionSequence(cmd)
		if err != nil {
			state.Put("error", fmt.Errorf("parse boot_command[%d] %q: %w", i, cmd, err))
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		default:
		}
		if err := seq.Do(ctx, driver); err != nil {
			// Connection dropped mid-keystroke (not during a silent <waitXs>).
			// Pass currentConn as old so WaitForNewConn correctly blocks until
			// the auto-reconnect goroutine delivers a truly new conn.
			ui.Say(fmt.Sprintf("boot_command[%d] failed: %s — waiting for VNC reconnect...", i, err))
			newConn, waitErr := ss.WaitForNewConn(ctx, currentConn)
			if waitErr != nil {
				state.Put("error", fmt.Errorf("send boot_command[%d]: %w", i, err))
				ui.Error(err.Error())
				return multistep.ActionHalt
			}
			driver = bootcommand.NewVNCDriver(newConn, s.Config.BootKeyInterval)
			ui.Say(fmt.Sprintf("VNC reconnected; retrying boot_command[%d]...", i))
			if retryErr := seq.Do(ctx, driver); retryErr != nil {
				state.Put("error", fmt.Errorf("send boot_command[%d] after reconnect: %w", i, retryErr))
				ui.Error(retryErr.Error())
				return multistep.ActionHalt
			}
		}
	}

	ui.Say("VNC boot commands sent")
	// Release the upstream VNC connection and local listener so the Sylve
	// WebUI can reclaim the VNC port. StepRestartAfterInstall will reconnect
	// explicitly after StartVM confirms the new bhyve is running.
	ss.Stop()
	return multistep.ActionContinue
}

func (s *StepVNCBootCommand) Cleanup(_ multistep.StateBag) {}
