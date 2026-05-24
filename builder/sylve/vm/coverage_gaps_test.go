// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packer "github.com/hashicorp/packer-plugin-sdk/packer"
	gossh "golang.org/x/crypto/ssh"

	sylvecommon "github.com/xoro/packer-plugin-sylve/builder/sylve/common"
	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func TestBuilder_Run_EnsureAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"vm_name":         "my-vm",
		"source_template": "base-template",
		"sylve_url":       srv.URL,
		"sylve_user":      "alice",
		"sylve_password":  "wrong",
		"communicator":    "none",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	_, runErr := b.Run(context.Background(), packer.TestUi(t), &packer.MockHook{})
	if runErr == nil {
		t.Fatal("expected Run error when login fails")
	}
}

func TestEnsureAuth_Logout_ErrorReported(t *testing.T) {
	origRetry := sylveLoginRetryInterval
	sylveLoginRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() { sylveLoginRetryInterval = origRetry })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "session-token"},
			})
		case "/api/auth/logout":
			http.Error(w, "logout failed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	b := &Builder{config: Config{
		SylveURL:                srv.URL,
		SylveUser:               "alice",
		SylvePassword:           "secret",
		SylveAuthType:           "sylve",
		TLSSkipVerify:           true,
		sylveAPILoginTimeoutDur: 30 * time.Second,
	}}

	cleanup, err := b.ensureAuth(packer.TestUi(t))
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected logout cleanup callback")
	}
	cleanup()
}

func TestEnsureAuth_ZeroWaitBudgetUsesDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	b := &Builder{config: Config{
		SylveURL:                srv.URL,
		SylveUser:               "alice",
		SylvePassword:           "wrong",
		SylveAuthType:           "sylve",
		TLSSkipVerify:           true,
		sylveAPILoginTimeoutDur: 0,
	}}

	_, err := b.ensureAuth(packer.TestUi(t))
	if err == nil {
		t.Fatal("expected login failure")
	}
}

func TestEnsureAuth_SleepClampedToRemainingDeadline(t *testing.T) {
	origRetry := sylveLoginRetryInterval
	sylveLoginRetryInterval = time.Hour
	t.Cleanup(func() { sylveLoginRetryInterval = origRetry })

	origLogin := ensureAuthLoginFn
	t.Cleanup(func() { ensureAuthLoginFn = origLogin })
	var attempts int
	ensureAuthLoginFn = func(_ *client.Client, _, _, _ string) (string, error) {
		attempts++
		return "", errors.New(`execute request POST /auth/login: API error 503 on POST /auth/login: {}`)
	}

	b := &Builder{config: Config{
		SylveURL:                "http://unused.example",
		SylveUser:               "alice",
		SylvePassword:           "secret",
		SylveAuthType:           "sylve",
		TLSSkipVerify:           true,
		sylveAPILoginTimeoutDur: 40 * time.Millisecond,
	}}

	start := time.Now()
	_, err := b.ensureAuth(packer.TestUi(t))
	if err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("retry sleep should respect deadline, took %v", time.Since(start))
	}
	if attempts < 2 {
		t.Fatalf("expected multiple attempts, got %d", attempts)
	}
}

func TestStepWinRMTunnel_MalformedSylveURL_Skip(t *testing.T) {
	cfg := &Config{SylveURL: "://invalid"}
	cfg.Config.Type = "winrm"
	state := newTestState(t)
	state.Put("instance_ip", "10.0.0.5")
	step := &StepWinRMTunnel{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
}

func TestStepWinRMTunnel_DialFailure_Halt(t *testing.T) {
	origLocal := sylveHostIsLocalFn
	t.Cleanup(func() { sylveHostIsLocalFn = origLocal })
	sylveHostIsLocalFn = func(_ string) bool { return false }

	origDial := dialBastionSSHFn
	t.Cleanup(func() { dialBastionSSHFn = origDial })
	dialBastionSSHFn = func(_, _, _ string, _ bool) (*gossh.Client, error) {
		return nil, errors.New("dial refused")
	}

	cfg := &Config{SylveURL: "https://remote.sylve.invalid:8181"}
	cfg.Config.Type = "winrm"
	state := newTestState(t)
	state.Put("instance_ip", "10.200.0.5")
	step := &StepWinRMTunnel{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected halt on dial failure")
	}
}

func TestStepWinRMTunnel_ListenFailure_Halt(t *testing.T) {
	origLocal := sylveHostIsLocalFn
	t.Cleanup(func() { sylveHostIsLocalFn = origLocal })
	sylveHostIsLocalFn = func(_ string) bool { return false }

	sshAddr := newTestSSHServer(t)
	origDial := dialBastionSSHFn
	t.Cleanup(func() { dialBastionSSHFn = origDial })
	dialBastionSSHFn = func(_, _, _ string, _ bool) (*gossh.Client, error) {
		clientCfg := &gossh.ClientConfig{
			User:            "testuser",
			Auth:            []gossh.AuthMethod{},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
		}
		return gossh.Dial("tcp", sshAddr, clientCfg)
	}

	origListen := winRMTunnelListenFn
	t.Cleanup(func() { winRMTunnelListenFn = origListen })
	winRMTunnelListenFn = func(_, _ string) (net.Listener, error) {
		return nil, errors.New("listen denied")
	}

	cfg := &Config{SylveURL: "https://remote.sylve.invalid:8181"}
	cfg.Config.Type = "winrm"
	state := newTestState(t)
	state.Put("instance_ip", "10.200.0.5")
	step := &StepWinRMTunnel{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected halt on listen failure")
	}
}

func TestStepWinRMTunnel_DefaultWinRMPort5985(t *testing.T) {
	echoAddr := newEchoServer(t)
	sshAddr := newTestSSHServer(t)

	origDial := dialBastionSSHFn
	t.Cleanup(func() { dialBastionSSHFn = origDial })
	dialBastionSSHFn = func(_, _, _ string, _ bool) (*gossh.Client, error) {
		clientCfg := &gossh.ClientConfig{
			User:            "testuser",
			Auth:            []gossh.AuthMethod{},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
		}
		return gossh.Dial("tcp", sshAddr, clientCfg)
	}

	origLocal := sylveHostIsLocalFn
	t.Cleanup(func() { sylveHostIsLocalFn = origLocal })
	sylveHostIsLocalFn = func(_ string) bool { return false }

	echoTCPAddr, _ := net.ResolveTCPAddr("tcp", echoAddr)
	cfg := &Config{SylveURL: "https://sylve.test.invalid:8181"}
	cfg.Config = communicator.Config{
		Type: "winrm",
		WinRM: communicator.WinRM{
			WinRMPort: 0,
		},
	}

	state := newTestState(t)
	state.Put("instance_ip", echoTCPAddr.IP.String())

	step := &StepWinRMTunnel{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
	step.Cleanup(state)
}

func TestStepWinRMTunnel_ForwardConns_DialError(t *testing.T) {
	sshAddr := newTestSSHServer(t)
	clientCfg := &gossh.ClientConfig{
		User:            "testuser",
		Auth:            []gossh.AuthMethod{},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
	}
	sshConn, err := gossh.Dial("tcp", sshAddr, clientCfg)
	if err != nil {
		t.Fatalf("dial ssh: %v", err)
	}
	t.Cleanup(func() { _ = sshConn.Close() })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	step := &StepWinRMTunnel{}
	go step.forwardConns(ln, sshConn, "127.0.0.1:1")

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	_ = conn.Close()
	time.Sleep(20 * time.Millisecond)
}

func TestStepDiscoverIP_PollErrorContinues(t *testing.T) {
	origTimeout := sylvecommon.DiscoverIPTotalTimeout
	origPoll := sylvecommon.DiscoverIPPollInterval
	sylvecommon.DiscoverIPTotalTimeout = 80 * time.Millisecond
	sylvecommon.DiscoverIPPollInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		sylvecommon.DiscoverIPTotalTimeout = origTimeout
		sylvecommon.DiscoverIPPollInterval = origPoll
	})

	var calls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/network/dhcp/lease", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "db down", http.StatusInternalServerError)
			return
		}
		type lease struct {
			MAC string `json:"mac"`
			IP  string `json:"ip"`
		}
		type leases struct {
			File []lease `json:"file"`
		}
		resp := map[string]interface{}{
			"status": "success",
			"data":   leases{File: []lease{{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.99"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_mac", "aa:bb:cc:dd:ee:ff")

	step := &sylvecommon.StepDiscoverIP{SylveURL: cfg.SylveURL, SylveToken: cfg.SylveToken, TLSSkipVerify: cfg.TLSSkipVerify}
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
	if ip, _ := state.Get("instance_ip").(string); ip != "10.0.0.99" {
		t.Fatalf("instance_ip = %q", ip)
	}
}

func TestStepDiscoverIP_ContextCancel(t *testing.T) {
	origPoll := sylvecommon.DiscoverIPPollInterval
	sylvecommon.DiscoverIPPollInterval = 50 * time.Millisecond
	t.Cleanup(func() { sylvecommon.DiscoverIPPollInterval = origPoll })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/network/dhcp/lease", func(w http.ResponseWriter, _ *http.Request) {
		type leases struct {
			File []interface{} `json:"file"`
		}
		resp := map[string]interface{}{"status": "success", "data": leases{File: []interface{}{}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_mac", "aa:bb:cc:dd:ee:ff")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()

	step := &sylvecommon.StepDiscoverIP{SylveURL: cfg.SylveURL, SylveToken: cfg.SylveToken, TLSSkipVerify: cfg.TLSSkipVerify}
	if step.Run(ctx, state) != multistep.ActionHalt {
		t.Fatal("expected halt on context cancel")
	}
}

func TestStepDeleteVM_Destroy_APIErrorContinues(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/12", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, "delete failed", http.StatusConflict)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true, KeepRegistered: false}
	state := newTestState(t)
	state.Put("vm_rid", uint(12))

	step := &sylvecommon.StepDeleteVM{SylveURL: cfg.SylveURL, SylveToken: cfg.SylveToken, TLSSkipVerify: cfg.TLSSkipVerify, Destroy: !cfg.KeepRegistered}
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("delete errors should not halt; err=%v", state.Get("error"))
	}
	if rid, _ := state.Get("vm_rid").(uint); rid != 12 {
		t.Fatalf("vm_rid should remain when delete fails, got %d", rid)
	}
	step.Cleanup(state)
}

func TestAllSteps_CleanupNoOps_Extended(t *testing.T) {
	state := newTestState(t)
	cfg := &Config{}
	(&StepShutdown{Config: cfg}).Cleanup(state)
}

func TestConfig_Prepare_LocalHostDebugPath(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	raw := vmMinimalValid()
	raw["sylve_url"] = "https://127.0.0.1:8181"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "" {
		t.Fatalf("SSHBastionHost = %q", c.Config.SSHBastionHost)
	}
}

func TestBuilder_Run_WithLoginCleanup(t *testing.T) {
	t.Cleanup(func() { vmBuildStepsHook = nil })
	vmBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	origRetry := sylveLoginRetryInterval
	sylveLoginRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() { sylveLoginRetryInterval = origRetry })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "session-token"},
			})
		case "/api/auth/logout":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"vm_name":         "my-vm",
		"source_template": "base-template",
		"sylve_url":       srv.URL,
		"sylve_user":      "alice",
		"sylve_password":  "secret",
		"communicator":    "none",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	art, runErr := b.Run(context.Background(), packer.TestUi(t), &packer.MockHook{})
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
	if art == nil {
		t.Fatal("nil artifact")
	}
}

func TestStepWinRMTunnel_InstanceIPWrongType_Halt(t *testing.T) {
	cfg := &Config{SylveURL: "https://remote.invalid:8181"}
	cfg.Config.Type = "winrm"
	state := newTestState(t)
	state.Put("instance_ip", 12345)
	step := &StepWinRMTunnel{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected halt when instance_ip is not a string")
	}
}

func TestStepDiscoverIP_EmptyMAC_ErrorMessage(t *testing.T) {
	step := &sylvecommon.StepDiscoverIP{}
	state := newTestState(t)
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected halt")
	}
	if err := state.Get("error"); err == nil {
		t.Fatal("expected error in state")
	}
}

func TestStepDeleteVM_KeepRegisteredTrue_Skip(t *testing.T) {
	cfg := &Config{KeepRegistered: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(99))
	if (&sylvecommon.StepDeleteVM{Destroy: !cfg.KeepRegistered}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatal("expected continue")
	}
}

func TestConfig_Prepare_DefaultSylveURLWhenUnset(t *testing.T) {
	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "tok")
	c := &Config{}
	_, _, err := c.Prepare(map[string]interface{}{
		"vm_name":         "vm1",
		"source_template": "base-template",
		"communicator":    "ssh",
		"ssh_username":    "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if c.SylveURL != "https://localhost:8181" {
		t.Fatalf("SylveURL = %q", c.SylveURL)
	}
}

func TestStepStartVM_Cleanup_NoVMRID(t *testing.T) {
	(&StepStartVM{Config: &Config{}}).Cleanup(newTestState(t))
}

func TestStepWinRMTunnel_ForwardConns_AcceptAfterClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	step := &StepWinRMTunnel{}
	done := make(chan struct{})
	go func() {
		step.forwardConns(ln, nil, "127.0.0.1:9")
		close(done)
	}()
	_ = ln.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forwardConns did not exit after listener close")
	}
}

func TestResolveBastionSSHParams_DefaultKeyFromHome(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	generateTestPrivateKey(t, keyPath)
	t.Setenv("HOME", tmp)

	_, keyFile, agentAuth := resolveBastionSSHParams("192.0.2.1")
	if keyFile != keyPath {
		t.Fatalf("keyFile = %q, want %q", keyFile, keyPath)
	}
	if agentAuth {
		t.Fatal("agentAuth should be false")
	}
}

func TestStepStartVM_Cleanup_StopTimeoutWarns(t *testing.T) {
	origPoll := startVMPollInterval
	origCleanupMax := startVMCleanupMaxWait
	startVMPollInterval = 5 * time.Millisecond
	startVMCleanupMaxWait = 25 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMCleanupMaxWait = origCleanupMax
	})

	rid := uint(104)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple/104", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"rid": rid, "state": int(client.DomainStateRunning),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/104", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	(&StepStartVM{Config: cfg}).Cleanup(state)
}

func TestEnsureAuth_MillisecondSleepWhenRemainingPositive(t *testing.T) {
	origRetry := sylveLoginRetryInterval
	sylveLoginRetryInterval = 0
	t.Cleanup(func() { sylveLoginRetryInterval = origRetry })

	origLogin := ensureAuthLoginFn
	t.Cleanup(func() { ensureAuthLoginFn = origLogin })
	var attempts int
	ensureAuthLoginFn = func(_ *client.Client, _, _, _ string) (string, error) {
		attempts++
		return "", errors.New(`execute request POST /auth/login: API error 503 on POST /auth/login: {}`)
	}

	b := &Builder{config: Config{
		SylveURL:                "http://unused.example",
		SylveUser:               "alice",
		SylvePassword:           "secret",
		SylveAuthType:           "sylve",
		TLSSkipVerify:           true,
		sylveAPILoginTimeoutDur: 5 * time.Millisecond,
	}}

	start := time.Now()
	_, err := b.ensureAuth(packer.TestUi(t))
	if err == nil {
		t.Fatal("expected timeout")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("expected fast timeout with zero retry interval, took %v", time.Since(start))
	}
	if attempts < 2 {
		t.Fatalf("expected multiple attempts, got %d", attempts)
	}
}

func TestConfig_Prepare_ExplicitSSH_BastionHostSkipsAuto(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	raw["ssh_bastion_host"] = "jump.example.com"
	raw["ssh_bastion_agent_auth"] = true
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "jump.example.com" {
		t.Fatalf("SSHBastionHost = %q", c.Config.SSHBastionHost)
	}
}
