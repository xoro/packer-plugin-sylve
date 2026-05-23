// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylvevm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"golang.org/x/crypto/ssh/agent"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func vmMinimalValid() map[string]interface{} {
	return map[string]interface{}{
		"vm_name":      "my-vm",
		"sylve_token":  "tok",
		"communicator": "ssh",
		"ssh_username": "root",
	}
}

func prepareVM(raw map[string]interface{}) (*Config, error) {
	c := &Config{}
	_, _, err := c.Prepare(raw)
	return c, err
}

func generateTestPrivateKey(t *testing.T, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal test key: %v", err)
	}
	f, err := os.Create(keyPath)
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}); err != nil {
		t.Fatalf("encode test key: %v", err)
	}
}

func TestSylveHostIsLocal_LocalhostVariants(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		if !sylveHostIsLocal(host) {
			t.Fatalf("sylveHostIsLocal(%q) = false, want true", host)
		}
	}
}

func TestSylveHostIsLocal_InterfaceAddrsError(t *testing.T) {
	orig := interfaceAddrsFn
	t.Cleanup(func() { interfaceAddrsFn = orig })
	interfaceAddrsFn = func() ([]net.Addr, error) {
		return nil, errors.New("no interfaces")
	}
	if sylveHostIsLocal("192.0.2.50") {
		t.Fatal("expected false when interfaceAddrs fails")
	}
}

func TestSylveHostIsLocal_IPAddrTypeBranch(t *testing.T) {
	orig := interfaceAddrsFn
	t.Cleanup(func() { interfaceAddrsFn = orig })
	interfaceAddrsFn = func() ([]net.Addr, error) {
		return []net.Addr{&net.IPAddr{IP: net.ParseIP("192.0.2.77")}}, nil
	}
	if !sylveHostIsLocal("192.0.2.77") {
		t.Fatal("expected local match via IPAddr branch")
	}
}

func TestSylveHostIsLocal_UnresolvableHost(t *testing.T) {
	if sylveHostIsLocal("this.host.does.not.exist.invalid") {
		t.Fatal("expected false for unresolvable host")
	}
}

func TestSshConfigForHost_UserHomeDirError(t *testing.T) {
	orig := userHomeDirFn
	t.Cleanup(func() { userHomeDirFn = orig })
	userHomeDirFn = func() (string, error) {
		return "", errors.New("no home")
	}
	if u, k, p := sshConfigForHost("host"); u != "" || k != "" || p != "" {
		t.Fatalf("got user=%q key=%q jump=%q", u, k, p)
	}
}

func TestSshConfigForHost_ExactMatch(t *testing.T) {
	cfg := `
Host myhost.example.com
  User deploy
  IdentityFile ~/.ssh/id_ed25519
`
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ssh", "config"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	user, keyFile, _ := sshConfigForHost("myhost.example.com")
	if user != "deploy" {
		t.Errorf("user = %q, want deploy", user)
	}
	wantKey := filepath.Join(tmp, ".ssh", "id_ed25519")
	if keyFile != wantKey {
		t.Errorf("identityFile = %q, want %q", keyFile, wantKey)
	}
}

func TestSshConfigForHost_WildcardMatch(t *testing.T) {
	cfg := `
Host *.example.com
  User wildcard
`
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ssh", "config"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	user, _, _ := sshConfigForHost("other.example.com")
	if user != "wildcard" {
		t.Errorf("user = %q, want wildcard", user)
	}
}

func TestSshConfigForHost_NoMatch(t *testing.T) {
	cfg := `
Host someother.host
  User nobody
`
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ssh", "config"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	user, keyFile, _ := sshConfigForHost("192.0.2.1")
	if user != "" || keyFile != "" {
		t.Errorf("expected empty results, got user=%q keyFile=%q", user, keyFile)
	}
}

func TestSshConfigForHost_ShortLineSkipped(t *testing.T) {
	cfg := `
Host short.example.com
  User
  IdentityFile ~/.ssh/id_ed25519
`
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ssh", "config"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	user, keyFile, _ := sshConfigForHost("short.example.com")
	if user != "" {
		t.Errorf("malformed User line should not set user, got %q", user)
	}
	wantKey := filepath.Join(tmp, ".ssh", "id_ed25519")
	if keyFile != wantKey {
		t.Errorf("identityFile = %q, want %q", keyFile, wantKey)
	}
}

func TestSshConfigForHost_InvalidHostPatternDoesNotMatch(t *testing.T) {
	cfg := `
Host [
  User shouldnotapply
`
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ssh", "config"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	user, _, _ := sshConfigForHost("any.host")
	if user != "" {
		t.Errorf("user = %q, want empty (invalid Host pattern)", user)
	}
}

func TestSshConfigForHost_ProxyJump(t *testing.T) {
	cfg := `
Host sylve.example.com
  User palltimo
  IdentityFile ~/.ssh/id_ed25519
  ProxyJump jumpbox.example.com
`
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ssh", "config"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	user, keyFile, proxyJump := sshConfigForHost("sylve.example.com")
	if user != "palltimo" {
		t.Errorf("user = %q, want palltimo", user)
	}
	wantKey := filepath.Join(tmp, ".ssh", "id_ed25519")
	if keyFile != wantKey {
		t.Errorf("identityFile = %q, want %q", keyFile, wantKey)
	}
	if proxyJump != "jumpbox.example.com" {
		t.Errorf("proxyJump = %q, want jumpbox.example.com", proxyJump)
	}
}

func TestConfig_SSHProxy_NotApplied_WhenLocalhost(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	raw := vmMinimalValid()
	raw["sylve_url"] = "https://localhost:8181"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "" {
		t.Errorf("SSHBastionHost = %q, want empty (local Sylve host)", c.Config.SSHBastionHost)
	}
}

func TestConfig_SSHProxy_Applied_WhenRemoteHost(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	t.Setenv("USER", "testuser")
	t.Setenv("HOME", t.TempDir())
	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "192.0.2.1" {
		t.Errorf("SSHBastionHost = %q, want 192.0.2.1", c.Config.SSHBastionHost)
	}
	if c.Config.SSHBastionUsername != "testuser" {
		t.Errorf("SSHBastionUsername = %q, want testuser", c.Config.SSHBastionUsername)
	}
	if !c.Config.SSHBastionAgentAuth {
		t.Error("SSHBastionAgentAuth = false, want true (no key available)")
	}
}

func TestConfig_SSHProxy_UsesSYLVE_SSH_PROXY_KEY(t *testing.T) {
	t.Setenv("USER", "testuser")
	home := t.TempDir()
	t.Setenv("HOME", home)
	keyPath := filepath.Join(home, "bastion-from-env.pem")
	generateTestPrivateKey(t, keyPath)
	t.Setenv("SYLVE_SSH_PROXY_KEY", keyPath)

	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionPrivateKeyFile != keyPath {
		t.Fatalf("SSHBastionPrivateKeyFile = %q, want %q", c.Config.SSHBastionPrivateKeyFile, keyPath)
	}
	if c.Config.SSHBastionAgentAuth {
		t.Error("SSHBastionAgentAuth = true, want false when SYLVE_SSH_PROXY_KEY is set")
	}
}

func TestConfig_SSHProxy_UsesSSHConfigIdentityFile(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	generateTestPrivateKey(t, keyPath)
	cfg := "Host 192.0.2.1\n  User sshconfiguser\n  IdentityFile ~/.ssh/id_ed25519\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionUsername != "sshconfiguser" {
		t.Errorf("SSHBastionUsername = %q, want sshconfiguser", c.Config.SSHBastionUsername)
	}
	if c.Config.SSHBastionPrivateKeyFile != keyPath {
		t.Errorf("SSHBastionPrivateKeyFile = %q, want %q", c.Config.SSHBastionPrivateKeyFile, keyPath)
	}
}

func TestConfig_SSHProxy_DefaultKeyProbeSkipsUnparseableKeyFile(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("not a private key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rsaKey := filepath.Join(sshDir, "id_rsa")
	generateTestPrivateKey(t, rsaKey)
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionPrivateKeyFile != rsaKey {
		t.Fatalf("SSHBastionPrivateKeyFile = %q, want %q", c.Config.SSHBastionPrivateKeyFile, rsaKey)
	}
}

func TestConfig_SSHProxy_ProxyJumpNoneDoesNotBlockPrepare(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfgText := "Host 192.0.2.1\n  User jumpuser\n  IdentityFile ~/.ssh/id_ed25519\n  ProxyJump none\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfgText), 0600); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	generateTestPrivateKey(t, keyPath)
	t.Setenv("HOME", tmp)

	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionUsername != "jumpuser" {
		t.Fatalf("SSHBastionUsername = %q", c.Config.SSHBastionUsername)
	}
}

func TestConfig_SSHProxy_SkipsWhenBastionUsernamePreset(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	t.Setenv("HOME", t.TempDir())
	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	raw["ssh_bastion_username"] = "preset-jump-user"
	raw["ssh_bastion_agent_auth"] = true
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionUsername != "preset-jump-user" {
		t.Fatalf("SSHBastionUsername = %q", c.Config.SSHBastionUsername)
	}
}

func TestConfig_Prepare_SkipsAutoBastionForWinRM(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	t.Setenv("HOME", t.TempDir())
	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	raw["communicator"] = "winrm"
	raw["winrm_username"] = "Administrator"
	raw["winrm_password"] = "secret"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "" {
		t.Errorf("SSHBastionHost = %q, want empty for winrm communicator", c.Config.SSHBastionHost)
	}
}

func TestResolveBastionSSHParams_SSHConfigKey(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	generateTestPrivateKey(t, keyPath)
	cfgText := "Host 192.0.2.1\n  User tunneluser\n  IdentityFile ~/.ssh/id_ed25519\n  ProxyJump jump.example.com\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfgText), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	user, keyFile, agentAuth := resolveBastionSSHParams("192.0.2.1")
	if user != "tunneluser" {
		t.Errorf("user = %q, want tunneluser", user)
	}
	if keyFile != keyPath {
		t.Errorf("keyFile = %q, want %q", keyFile, keyPath)
	}
	if agentAuth {
		t.Error("agentAuth should be false when key file resolved")
	}
}

func TestResolveBastionSSHParams_AgentFallback(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USER", "tunneluser")

	_, keyFile, agentAuth := resolveBastionSSHParams("192.0.2.1")
	if keyFile != "" {
		t.Errorf("keyFile = %q, want empty", keyFile)
	}
	if !agentAuth {
		t.Error("agentAuth = false, want true when no keys found")
	}
}

func TestDialBastionSSH_WithKeyFile(t *testing.T) {
	sshAddr := newTestSSHServer(t)
	home := t.TempDir()
	keyPath := filepath.Join(home, "client.pem")
	generateTestPrivateKey(t, keyPath)

	orig := bastionSSHDialAddrForTests
	t.Cleanup(func() { bastionSSHDialAddrForTests = orig })
	bastionSSHDialAddrForTests = sshAddr

	client, err := dialBastionSSH("unused-host", "testuser", keyPath, false)
	if err != nil {
		t.Fatalf("dialBastionSSH: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
}

func TestDialBastionSSH_ReadKeyError(t *testing.T) {
	_, err := dialBastionSSH("127.0.0.1", "u", "/no/such/key.pem", false)
	if err == nil {
		t.Fatal("expected read key error")
	}
}

func TestDialBastionSSH_ParseKeyError(t *testing.T) {
	tmp := t.TempDir()
	keyPath := filepath.Join(tmp, "bad.pem")
	if err := os.WriteFile(keyPath, []byte("not-a-key"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := dialBastionSSH("127.0.0.1", "u", keyPath, false)
	if err == nil {
		t.Fatal("expected parse key error")
	}
}

func TestDialBastionSSH_AgentAuthMissingSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	_, err := dialBastionSSH("127.0.0.1", "u", "", true)
	if err == nil {
		t.Fatal("expected error when SSH_AUTH_SOCK unset")
	}
}

func TestApplyAutoBastion_ExplicitAuthFieldsPreserved(t *testing.T) {
	c := &Config{}
	c.Config.SSHBastionPassword = "preset"
	c.Config.SSHBastionPrivateKeyFile = "/preset/key"
	applyAutoBastion(c, "192.0.2.1")
	if c.Config.SSHBastionHost != "192.0.2.1" {
		t.Fatalf("SSHBastionHost = %q", c.Config.SSHBastionHost)
	}
	if c.Config.SSHBastionPrivateKeyFile != "/preset/key" {
		t.Fatalf("private key should not be overwritten")
	}
}

func newTestSSHAgentSocket(t *testing.T) string {
	t.Helper()
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	ring := agent.NewKeyring()
	if err := ring.Add(agent.AddedKey{PrivateKey: privKey}); err != nil {
		t.Fatalf("add key: %v", err)
	}
	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("sylve-agent-%d.sock", os.Getpid()))
	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(sockPath) })
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go agent.ServeAgent(ring, conn)
		}
	}()
	return ln.Addr().String()
}

func TestDialBastionSSH_AgentAuthSuccess(t *testing.T) {
	sshAddr := newTestSSHServer(t)
	t.Setenv("SSH_AUTH_SOCK", newTestSSHAgentSocket(t))

	orig := bastionSSHDialAddrForTests
	t.Cleanup(func() { bastionSSHDialAddrForTests = orig })
	bastionSSHDialAddrForTests = sshAddr

	conn, err := dialBastionSSH("remote.example", "testuser", "", true)
	if err != nil {
		t.Fatalf("dialBastionSSH with agent: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
}

func TestApplyAutoBastion_ProxyJumpWarningPath(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfgText := "Host 192.0.2.1\n  User jumpuser\n  IdentityFile ~/.ssh/id_ed25519\n  ProxyJump jump.example.com\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfgText), 0600); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	generateTestPrivateKey(t, keyPath)
	t.Setenv("HOME", tmp)
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")

	c := &Config{}
	applyAutoBastion(c, "192.0.2.1")
	if c.Config.SSHBastionUsername != "jumpuser" {
		t.Fatalf("SSHBastionUsername = %q", c.Config.SSHBastionUsername)
	}
}

func TestConfig_SSHProxy_DefaultKeyProbeSkipsDirectoryNamedLikeKey(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sshDir, "id_ed25519"), 0700); err != nil {
		t.Fatal(err)
	}
	rsaKey := filepath.Join(sshDir, "id_rsa")
	generateTestPrivateKey(t, rsaKey)
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionPrivateKeyFile != rsaKey {
		t.Fatalf("SSHBastionPrivateKeyFile = %q, want %q", c.Config.SSHBastionPrivateKeyFile, rsaKey)
	}
}

func TestConfig_Prepare_ExplicitTLSSkipVerifyTrue(t *testing.T) {
	raw := vmMinimalValid()
	raw["tls_skip_verify"] = true
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !c.TLSSkipVerify {
		t.Fatal("TLSSkipVerify should remain true when explicitly set")
	}
}

func TestConfig_Prepare_DefaultTLSSkipVerifyWhenUnset(t *testing.T) {
	raw := vmMinimalValid()
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !c.TLSSkipVerify {
		t.Fatal("TLSSkipVerify should default to true")
	}
}

func TestConfig_Prepare_SkipsAutoBastionForCommunicatorNone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	raw := vmMinimalValid()
	raw["sylve_url"] = "https://192.0.2.1:8181"
	raw["communicator"] = "none"
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "" {
		t.Fatalf("SSHBastionHost = %q", c.Config.SSHBastionHost)
	}
}

func TestConfig_Prepare_DestroyTrueKeepRegisteredFalse(t *testing.T) {
	raw := vmMinimalValid()
	raw["destroy"] = true
	raw["keep_registered"] = false
	c, err := prepareVM(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !c.Destroy || !c.KeepRegistered {
		t.Fatalf("destroy=%v keep_registered=%v", c.Destroy, c.KeepRegistered)
	}
}

func TestResolveBastionSSHParams_UsesEnvUserWhenSSHConfigEmpty(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "/tmp/key-from-env.pem")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USER", "envtunneluser")

	user, keyFile, _ := resolveBastionSSHParams("192.0.2.1")
	if user != "envtunneluser" {
		t.Fatalf("user = %q", user)
	}
	if keyFile != "/tmp/key-from-env.pem" {
		t.Fatalf("keyFile = %q", keyFile)
	}
}

func TestResolveBastionSSHParams_SkipsUnusableDefaultKeys(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	rsaKey := filepath.Join(sshDir, "id_rsa")
	generateTestPrivateKey(t, rsaKey)
	t.Setenv("HOME", tmp)

	_, keyFile, agentAuth := resolveBastionSSHParams("192.0.2.1")
	if keyFile != rsaKey {
		t.Fatalf("keyFile = %q, want %q", keyFile, rsaKey)
	}
	if agentAuth {
		t.Fatal("expected key file, not agent")
	}
}

func TestResolveBastionSSHParams_SkipsUnreadableDefaultKey(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(sshDir, "id_ed25519")
	if err := os.WriteFile(unreadable, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0000); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0600) })

	_, keyFile, agentAuth := resolveBastionSSHParams("192.0.2.1")
	if keyFile != "" {
		t.Fatalf("keyFile = %q, want empty", keyFile)
	}
	if !agentAuth {
		t.Fatal("expected agent auth when default keys are unreadable")
	}
}

func TestDialBastionSSH_AgentDialError(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(os.TempDir(), "nonexistent-agent.sock"))
	_, err := dialBastionSSH("127.0.0.1", "u", "", true)
	if err == nil {
		t.Fatal("expected agent dial error")
	}
}

func TestStepStartVM_Cleanup_PollErrorThenStopped(t *testing.T) {
	origPoll := startVMPollInterval
	startVMPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { startVMPollInterval = origPoll })

	rid := uint(105)
	var getN int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple/105", func(w http.ResponseWriter, _ *http.Request) {
		getN++
		if getN == 1 {
			resp := map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"rid": rid, "state": int(client.DomainStateRunning),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if getN == 2 {
			http.Error(w, "poll fail", http.StatusInternalServerError)
			return
		}
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"rid": rid, "state": int(client.DomainStateShutoff),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/105", func(w http.ResponseWriter, r *http.Request) {
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

func TestStepStartVM_Run_LogsOnSuccess(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTask := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = 2 * time.Second
	startVMTaskPoll = 10 * time.Millisecond
	startVMTaskMaxWait = 50 * time.Millisecond
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTask
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(106)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/106", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/106", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/106", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateRunning},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/106", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data":   map[string]string{"logs": "bhyve started"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", rid)
	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
}

func TestStepStartVM_Run_LogsDomainStateTransitions(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTask := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = 2 * time.Second
	startVMTaskPoll = 10 * time.Millisecond
	startVMTaskMaxWait = 50 * time.Millisecond
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTask
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(107)
	var simpleN int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/107", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/107", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/107", func(w http.ResponseWriter, _ *http.Request) {
		simpleN++
		st := client.DomainStateShutoff
		if simpleN >= 2 {
			st = client.DomainStateRunning
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: st},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/107", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": map[string]string{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", rid)
	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
	if simpleN < 2 {
		t.Fatalf("expected multiple state polls, got %d", simpleN)
	}
}

func TestStepStartVM_Cleanup_StoppedAfterSuccessfulStop(t *testing.T) {
	origPoll := startVMPollInterval
	startVMPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { startVMPollInterval = origPoll })

	rid := uint(108)
	var getN int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple/108", func(w http.ResponseWriter, _ *http.Request) {
		getN++
		st := int(client.DomainStateRunning)
		if getN >= 2 {
			st = int(client.DomainStateShutoff)
		}
		resp := map[string]interface{}{
			"status": "success",
			"data":   map[string]interface{}{"rid": rid, "state": st},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/108", func(w http.ResponseWriter, r *http.Request) {
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
	if getN < 2 {
		t.Fatalf("expected cleanup poll until shutoff, getN=%d", getN)
	}
}

func TestResolveBastionSSHParams_ProxyJumpWarningPath(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfgText := "Host 192.0.2.1\n  User tunneluser\n  ProxyJump jump.example.com\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfgText), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")

	user, _, agentAuth := resolveBastionSSHParams("192.0.2.1")
	if user != "tunneluser" {
		t.Fatalf("user = %q", user)
	}
	if !agentAuth {
		t.Fatal("expected agent auth fallback")
	}
}
