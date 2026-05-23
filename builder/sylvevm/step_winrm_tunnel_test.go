// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylvevm

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	gossh "golang.org/x/crypto/ssh"
)

// ---------------------------------------------------------------------------
// Run() skip-condition tests
// ---------------------------------------------------------------------------

func TestStepWinRMTunnel_SkipNonWinRM(t *testing.T) {
	cfg := &Config{}
	cfg.Config.Type = "ssh"
	step := &StepWinRMTunnel{Config: cfg}
	action := step.Run(context.Background(), newTestState(t))
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue for SSH communicator, got %v", action)
	}
}

func TestStepWinRMTunnel_SkipWinRMHostExplicit(t *testing.T) {
	cfg := &Config{}
	cfg.Config.Type = "winrm"
	cfg.Config.WinRMHost = "explicit.host.example"
	step := &StepWinRMTunnel{Config: cfg}
	action := step.Run(context.Background(), newTestState(t))
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue when winrm_host is already set, got %v", action)
	}
}

func TestStepWinRMTunnel_SkipLocalSylveURL(t *testing.T) {
	cfg := &Config{SylveURL: "https://127.0.0.1:8181"}
	cfg.Config.Type = "winrm"
	state := newTestState(t)
	state.Put("instance_ip", "10.200.0.5")
	step := &StepWinRMTunnel{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue for local Sylve host, got %v; error=%v", action, state.Get("error"))
	}
	// WinRMHost must not have been mutated.
	if cfg.Config.WinRMHost != "" {
		t.Errorf("WinRMHost was mutated to %q, expected empty", cfg.Config.WinRMHost)
	}
}

func TestStepWinRMTunnel_NoInstanceIP_Halt(t *testing.T) {
	// Use a hostname that fails DNS so sylveHostIsLocal returns false,
	// causing the step to reach the instance_ip check.
	cfg := &Config{SylveURL: "https://not.reachable.invalid:8181"}
	cfg.Config.Type = "winrm"
	// instance_ip deliberately not set in state.
	step := &StepWinRMTunnel{Config: cfg}
	action := step.Run(context.Background(), newTestState(t))
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt when instance_ip is missing, got %v", action)
	}
}

// ---------------------------------------------------------------------------
// Cleanup nil safety
// ---------------------------------------------------------------------------

func TestStepWinRMTunnel_Cleanup_NilSafe(t *testing.T) {
	step := &StepWinRMTunnel{Config: &Config{}}
	// Must not panic when listener and sshConn are nil.
	step.Cleanup(newTestState(t))
}

// ---------------------------------------------------------------------------
// dialBastionSSH error path
// ---------------------------------------------------------------------------

func TestDialBastionSSH_NoAuthMethod_Error(t *testing.T) {
	// keyFile="" and agentAuth=false: function must return an error without
	// attempting a network connection.
	_, err := dialBastionSSH("127.0.0.1", "testuser", "", false)
	if err == nil {
		t.Fatal("expected error when no auth method is available")
	}
}

// ---------------------------------------------------------------------------
// resolveBastionSSHParams: SYLVE_SSH_PROXY_KEY priority
// ---------------------------------------------------------------------------

func TestResolveBastionSSHParams_EnvKeyTakesPriority(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "/tmp/test-sylve-proxy-key.pem")
	_, keyFile, agentAuth := resolveBastionSSHParams("irrelevant.host")
	if keyFile != "/tmp/test-sylve-proxy-key.pem" {
		t.Errorf("keyFile = %q, want /tmp/test-sylve-proxy-key.pem", keyFile)
	}
	if agentAuth {
		t.Error("agentAuth should be false when a key file is provided")
	}
}

// ---------------------------------------------------------------------------
// forwardConns end-to-end: local SSH server + echo backend
// ---------------------------------------------------------------------------

// newTestSSHServer starts a minimal SSH server on 127.0.0.1 that accepts any
// client (NoClientAuth) and handles "direct-tcpip" channels by opening a TCP
// connection to the requested destination and copying data bidirectionally.
// Returns the server address.
func newTestSSHServer(t *testing.T) string {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := gossh.NewSignerFromKey(privKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	srvCfg := &gossh.ServerConfig{NoClientAuth: true}
	srvCfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveTestSSHConn(conn, srvCfg)
		}
	}()

	return ln.Addr().String()
}

// directTCPIPPayload mirrors the wire format of a "direct-tcpip" open payload.
type directTCPIPPayload struct {
	DestAddr string
	DestPort uint32
	SrcAddr  string
	SrcPort  uint32
}

func serveTestSSHConn(conn net.Conn, cfg *gossh.ServerConfig) {
	srvConn, chans, reqs, err := gossh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer srvConn.Close()
	go gossh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "direct-tcpip" {
			_ = newChan.Reject(gossh.UnknownChannelType, "unsupported")
			continue
		}
		var payload directTCPIPPayload
		if err := gossh.Unmarshal(newChan.ExtraData(), &payload); err != nil {
			_ = newChan.Reject(gossh.ConnectionFailed, "bad payload")
			continue
		}
		target, err := net.Dial("tcp", fmt.Sprintf("%s:%d", payload.DestAddr, payload.DestPort))
		if err != nil {
			_ = newChan.Reject(gossh.ConnectionFailed, err.Error())
			continue
		}
		ch, requests, err := newChan.Accept()
		if err != nil {
			_ = target.Close()
			continue
		}
		go gossh.DiscardRequests(requests)
		go func(ch gossh.Channel, target net.Conn) {
			defer ch.Close()
			defer target.Close()
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); _, _ = io.Copy(target, ch) }()
			go func() { defer wg.Done(); _, _ = io.Copy(ch, target) }()
			wg.Wait()
		}(ch, target)
	}
}

// newEchoServer starts a TCP echo server on 127.0.0.1 and returns its address.
func newEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return ln.Addr().String()
}

func TestStepWinRMTunnel_ForwardConns(t *testing.T) {
	echoAddr := newEchoServer(t)
	sshAddr := newTestSSHServer(t)

	// Dial the test SSH server as a client (no auth required).
	clientCfg := &gossh.ClientConfig{
		User:            "testuser",
		Auth:            []gossh.AuthMethod{},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
	}
	sshConn, err := gossh.Dial("tcp", sshAddr, clientCfg)
	if err != nil {
		t.Fatalf("dial test SSH server: %v", err)
	}
	t.Cleanup(func() { _ = sshConn.Close() })

	// Set up a tunnel listener and start forwardConns.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	step := &StepWinRMTunnel{}
	go step.forwardConns(ln, sshConn, echoAddr)

	// Connect through the tunnel and verify echo behaviour.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer conn.Close()

	want := []byte("hello winrm tunnel")
	if _, err := conn.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("echo mismatch: got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Run() full-path test using hooks
// ---------------------------------------------------------------------------

func TestStepWinRMTunnel_Run_TunnelEstablished(t *testing.T) {
	echoAddr := newEchoServer(t)
	sshAddr := newTestSSHServer(t)

	// Override the dial function to connect to the local test SSH server
	// regardless of the hostname supplied.
	orig := dialBastionSSHFn
	t.Cleanup(func() { dialBastionSSHFn = orig })
	dialBastionSSHFn = func(_, _, _ string, _ bool) (*gossh.Client, error) {
		clientCfg := &gossh.ClientConfig{
			User:            "testuser",
			Auth:            []gossh.AuthMethod{},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
		}
		return gossh.Dial("tcp", sshAddr, clientCfg)
	}

	// Override the local-host check so the "remote" hostname is not skipped.
	origLocal := sylveHostIsLocalFn
	t.Cleanup(func() { sylveHostIsLocalFn = origLocal })
	sylveHostIsLocalFn = func(_ string) bool { return false }

	// Build a config with the echo server's port as the WinRM target so the
	// tunnel connects there.
	echoTCPAddr, _ := net.ResolveTCPAddr("tcp", echoAddr)
	cfg := &Config{SylveURL: "https://sylve.test.invalid:8181"}
	cfg.Config = communicator.Config{
		Type: "winrm",
		WinRM: communicator.WinRM{
			WinRMPort: echoTCPAddr.Port,
		},
	}

	state := newTestState(t)
	state.Put("instance_ip", echoTCPAddr.IP.String())

	step := &StepWinRMTunnel{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}

	// WinRMHost must be 127.0.0.1 and WinRMPort must be a non-zero local port.
	if cfg.Config.WinRMHost != "127.0.0.1" {
		t.Errorf("WinRMHost = %q, want 127.0.0.1", cfg.Config.WinRMHost)
	}
	if cfg.Config.WinRMPort == 0 {
		t.Error("WinRMPort was not updated by the tunnel step")
	}

	// Verify data flows through the tunnel by connecting to the local port.
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Config.WinRMPort))
	if err != nil {
		t.Fatalf("dial tunnel port: %v", err)
	}
	defer conn.Close()

	want := []byte("winrm over ssh")
	if _, err := conn.Write(want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	step.Cleanup(state)
}
