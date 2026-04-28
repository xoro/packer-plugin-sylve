// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// minimalValid returns the smallest raw config map that passes Prepare without
// errors. Tests that exercise a specific field start from this base and add or
// override the field under test.
func minimalValid() map[string]interface{} {
	return map[string]interface{}{
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
}

func prepare(raw map[string]interface{}) (*Config, error) {
	c := &Config{}
	_, _, err := c.Prepare(raw)
	return c, err
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

func TestConfig_VNCHost_FallbackWhenURLHasNoHostname(t *testing.T) {
	t.Setenv("SYLVE_HOST", "")
	c, err := prepare(map[string]interface{}{
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
		"sylve_url":        "http://",
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.VNCHost != "127.0.0.1" {
		t.Fatalf("VNCHost = %q, want 127.0.0.1", c.VNCHost)
	}
}

func TestConfig_TLSExplicitFalseStillForcesInsecureSkip(t *testing.T) {
	c, err := prepare(map[string]interface{}{
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
		"tls_skip_verify":  false,
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !c.TLSSkipVerify {
		t.Fatal("Prepare should set TLSSkipVerify true for self-signed default")
	}
}

func TestConfig_Defaults_SylveURL(t *testing.T) {
	t.Setenv("SYLVE_HOST", "") // prevent session env from overriding default
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SylveURL != "https://localhost:8181" {
		t.Errorf("SylveURL = %q, want %q", c.SylveURL, "https://localhost:8181")
	}
}

func TestConfig_Defaults_SylveURL_FromEnv(t *testing.T) {
	t.Setenv("SYLVE_HOST", "myhost.example.com")
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://myhost.example.com:8181"
	if c.SylveURL != want {
		t.Errorf("SylveURL = %q, want %q", c.SylveURL, want)
	}
}

func TestConfig_Defaults_SylveURL_ExplicitWins(t *testing.T) {
	t.Setenv("SYLVE_HOST", "should-be-ignored.example.com")
	raw := minimalValid()
	raw["sylve_url"] = "https://custom.host:9999"
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SylveURL != "https://custom.host:9999" {
		t.Errorf("SylveURL = %q, want explicit value", c.SylveURL)
	}
}

func TestConfig_Defaults_AuthType(t *testing.T) {
	// Force the builtin default branch: if SYLVE_AUTH_TYPE is set in the
	// process environment, the "default to sylve" block in Prepare is skipped
	// and total coverage drops below the repo threshold.
	t.Setenv("SYLVE_AUTH_TYPE", "")
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SylveAuthType != "sylve" {
		t.Errorf("SylveAuthType = %q, want %q", c.SylveAuthType, "sylve")
	}
}

func TestConfig_Defaults_CPUCores(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.CPUCores != 2 {
		t.Errorf("CPUCores = %d, want 2", c.CPUCores)
	}
}

func TestConfig_Defaults_CPUSockets(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.CPUSockets != 1 {
		t.Errorf("CPUSockets = %d, want 1", c.CPUSockets)
	}
}

func TestConfig_Defaults_CPUThreads(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.CPUThreads != 1 {
		t.Errorf("CPUThreads = %d, want 1", c.CPUThreads)
	}
}

func TestConfig_Defaults_RAM(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.RAM != 1024 {
		t.Errorf("RAM = %d, want 1024", c.RAM)
	}
}

func TestConfig_Defaults_StorageSizeMB(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.StorageSizeMB != 65536 {
		t.Errorf("StorageSizeMB = %d, want 65536", c.StorageSizeMB)
	}
}

func TestConfig_Defaults_StorageType(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.StorageType != "zvol" {
		t.Errorf("StorageType = %q, want %q", c.StorageType, "zvol")
	}
}

func TestConfig_Defaults_StorageEmulationType(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.StorageEmulationType != "virtio-blk" {
		t.Errorf("StorageEmulationType = %q, want %q", c.StorageEmulationType, "virtio-blk")
	}
}

func TestConfig_Defaults_SwitchEmulationType(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SwitchEmulationType != "e1000" {
		t.Errorf("SwitchEmulationType = %q, want %q", c.SwitchEmulationType, "e1000")
	}
}

func TestConfig_Defaults_VNCPortMin(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.VNCPortMin != 5900 {
		t.Errorf("VNCPortMin = %d, want 5900", c.VNCPortMin)
	}
}

func TestConfig_Defaults_VNCPortMax(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.VNCPortMax != 5999 {
		t.Errorf("VNCPortMax = %d, want 5999", c.VNCPortMax)
	}
}

func TestConfig_Defaults_VNCResolution(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.VNCResolution != "1024x768" {
		t.Errorf("VNCResolution = %q, want %q", c.VNCResolution, "1024x768")
	}
}

func TestConfig_Defaults_Loader(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Loader != "uefi" {
		t.Errorf("Loader = %q, want %q", c.Loader, "uefi")
	}
}

func TestConfig_Defaults_TimeOffset(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.TimeOffset != "utc" {
		t.Errorf("TimeOffset = %q, want %q", c.TimeOffset, "utc")
	}
}

func TestConfig_Defaults_BootKeyInterval(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.BootKeyInterval != 200*time.Millisecond {
		t.Errorf("BootKeyInterval = %v, want 200ms", c.BootKeyInterval)
	}
}

func TestConfig_Defaults_ACPI_APIC(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.ACPI {
		t.Error("ACPI = false, want true (always enabled)")
	}
	if !c.APIC {
		t.Error("APIC = false, want true (always enabled)")
	}
}

func TestConfig_Defaults_ShutdownCommand(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ShutdownCommand != "/sbin/poweroff" {
		t.Errorf("ShutdownCommand = %q, want %q", c.ShutdownCommand, "/sbin/poweroff")
	}
}

func TestConfig_Defaults_TLSSkipVerify(t *testing.T) {
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.TLSSkipVerify {
		t.Error("TLSSkipVerify = false, want true (Sylve default self-signed cert)")
	}
}

// ---------------------------------------------------------------------------
// VNCHost derivation from SylveURL
// ---------------------------------------------------------------------------

func TestConfig_VNCHost_DerivedFromSylveURL(t *testing.T) {
	raw := minimalValid()
	raw["sylve_url"] = "https://192.168.1.10:8181"
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.VNCHost != "192.168.1.10" {
		t.Errorf("VNCHost = %q, want %q", c.VNCHost, "192.168.1.10")
	}
}

func TestConfig_VNCHost_DerivedFromSylveURL_Hostname(t *testing.T) {
	raw := minimalValid()
	raw["sylve_url"] = "https://sylve.home.example.com:8181"
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.VNCHost != "sylve.home.example.com" {
		t.Errorf("VNCHost = %q, want %q", c.VNCHost, "sylve.home.example.com")
	}
}

func TestConfig_VNCHost_ExplicitWins(t *testing.T) {
	raw := minimalValid()
	raw["sylve_url"] = "https://192.168.1.10:8181"
	raw["vnc_host"] = "10.0.0.1"
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.VNCHost != "10.0.0.1" {
		t.Errorf("VNCHost = %q, want explicit %q", c.VNCHost, "10.0.0.1")
	}
}

// ---------------------------------------------------------------------------
// Env-var fallbacks for auth fields
// ---------------------------------------------------------------------------

func TestConfig_EnvFallback_SylveToken(t *testing.T) {
	t.Setenv("SYLVE_TOKEN", "env-token")
	raw := map[string]interface{}{
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SylveToken != "env-token" {
		t.Errorf("SylveToken = %q, want %q", c.SylveToken, "env-token")
	}
}

func TestConfig_EnvFallback_UserPassword(t *testing.T) {
	t.Setenv("SYLVE_USER", "admin")
	t.Setenv("SYLVE_PASSWORD", "secret")
	raw := map[string]interface{}{
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SylveUser != "admin" {
		t.Errorf("SylveUser = %q, want %q", c.SylveUser, "admin")
	}
	if c.SylvePassword != "secret" {
		t.Errorf("SylvePassword = %q, want %q", c.SylvePassword, "secret")
	}
}

func TestConfig_EnvFallback_AuthType(t *testing.T) {
	t.Setenv("SYLVE_TOKEN", "tok")
	t.Setenv("SYLVE_AUTH_TYPE", "pam")
	if _, err := prepare(minimalValid()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Explicit field in minimalValid has no auth_type, so env should be used.
	// But note minimalValid sets sylve_token directly so the env is only
	// relevant when the field is empty.
	raw := map[string]interface{}{
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	t.Setenv("SYLVE_TOKEN", "tok")
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SylveAuthType != "pam" {
		t.Errorf("SylveAuthType = %q, want %q", c.SylveAuthType, "pam")
	}
}

// ---------------------------------------------------------------------------
// Validation errors
// ---------------------------------------------------------------------------

func TestConfig_Error_NoAuth(t *testing.T) {
	// Clear all auth env vars so session-level exports do not satisfy auth.
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")
	raw := map[string]interface{}{
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for missing auth, got nil")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("error %q does not mention 'authentication required'", err.Error())
	}
}

func TestConfig_Error_TokenOrUserPassword_Token(t *testing.T) {
	raw := minimalValid()
	_, err := prepare(raw)
	if err != nil {
		t.Fatalf("sylve_token alone should be sufficient: %v", err)
	}
}

func TestConfig_Error_TokenOrUserPassword_UserPass(t *testing.T) {
	raw := map[string]interface{}{
		"sylve_user":       "admin",
		"sylve_password":   "secret",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	_, err := prepare(raw)
	if err != nil {
		t.Fatalf("user+password alone should be sufficient: %v", err)
	}
}

func TestConfig_Error_UserWithoutPassword(t *testing.T) {
	t.Setenv("SYLVE_PASSWORD", "") // prevent session env from supplying a password
	raw := map[string]interface{}{
		"sylve_user":       "admin",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for user without password, got nil")
	}
}

func TestConfig_Error_NoISOURL(t *testing.T) {
	raw := map[string]interface{}{
		"sylve_token":  "tok",
		"switch_name":  "packer-switch",
		"ssh_username": "root",
	}
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for missing iso_download_url, got nil")
	}
	if !strings.Contains(err.Error(), "iso_download_url") {
		t.Errorf("error %q does not mention 'iso_download_url'", err.Error())
	}
}

func TestConfig_Error_VNCPortMinBelow5900(t *testing.T) {
	raw := minimalValid()
	raw["vnc_port_min"] = 1234
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for vnc_port_min < 5900, got nil")
	}
	if !strings.Contains(err.Error(), "5900") {
		t.Errorf("error %q does not mention '5900'", err.Error())
	}
}

func TestConfig_Error_VNCPortMaxLessThanMin(t *testing.T) {
	raw := minimalValid()
	raw["vnc_port_min"] = 5950
	raw["vnc_port_max"] = 5900
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for vnc_port_max < vnc_port_min, got nil")
	}
	if !strings.Contains(err.Error(), "vnc_port_max") {
		t.Errorf("error %q does not mention 'vnc_port_max'", err.Error())
	}
}

func TestConfig_Error_InvalidBootWait(t *testing.T) {
	raw := minimalValid()
	raw["boot_wait"] = "not-a-duration"
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for invalid boot_wait, got nil")
	}
	if !strings.Contains(err.Error(), "boot_wait") {
		t.Errorf("error %q does not mention 'boot_wait'", err.Error())
	}
}

func TestConfig_HTTPPrepareInvalidPortRange(t *testing.T) {
	raw := minimalValid()
	raw["http_port_min"] = 9000
	raw["http_port_max"] = 8000
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error from HTTPConfig.Prepare for invalid http port range, got nil")
	}
}

func TestConfig_CommunicatorPrepareInvalidSSHTimeout(t *testing.T) {
	raw := minimalValid()
	raw["ssh_timeout"] = "not-a-duration"
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error from communicator.Config.Prepare for invalid ssh_timeout, got nil")
	}
}

// ---------------------------------------------------------------------------
// bootWaitDuration
// ---------------------------------------------------------------------------

func TestConfig_BootWaitDuration_Empty(t *testing.T) {
	c := &Config{}
	d, err := c.bootWaitDuration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 10*time.Second {
		t.Errorf("bootWaitDuration() = %v, want 10s", d)
	}
}

func TestConfig_BootWaitDuration_Valid(t *testing.T) {
	c := &Config{BootWait: "2m30s"}
	d, err := c.bootWaitDuration()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := 2*time.Minute + 30*time.Second
	if d != want {
		t.Errorf("bootWaitDuration() = %v, want %v", d, want)
	}
}

func TestConfig_BootWaitDuration_Invalid(t *testing.T) {
	c := &Config{BootWait: "bogus"}
	_, err := c.bootWaitDuration()
	if err == nil {
		t.Fatal("expected error for invalid duration, got nil")
	}
}

// ---------------------------------------------------------------------------
// Explicit values are preserved (not overwritten by defaults)
// ---------------------------------------------------------------------------

func TestConfig_ExplicitValues_Preserved(t *testing.T) {
	raw := map[string]interface{}{
		"sylve_token":            "tok",
		"iso_download_url":       "https://example.com/os.iso",
		"switch_name":            "packer-switch",
		"ssh_username":           "root",
		"cpu_cores":              4,
		"cpu_sockets":            2,
		"cpu_threads":            2,
		"ram":                    4096,
		"storage_size_mb":        10240,
		"storage_type":           "custom-zvol",
		"storage_emulation_type": "ahci-hd",
		"switch_emulation_type":  "virtio",
		"vnc_port_min":           5910,
		"vnc_port_max":           5920,
		"vnc_resolution":         "1920x1080",
		"loader":                 "bios",
		"time_offset":            "localtime",
		"shutdown_command":       "/usr/sbin/poweroff",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.CPUCores != 4 {
		t.Errorf("CPUCores = %d, want 4", c.CPUCores)
	}
	if c.CPUSockets != 2 {
		t.Errorf("CPUSockets = %d, want 2", c.CPUSockets)
	}
	if c.CPUThreads != 2 {
		t.Errorf("CPUThreads = %d, want 2", c.CPUThreads)
	}
	if c.RAM != 4096 {
		t.Errorf("RAM = %d, want 4096", c.RAM)
	}
	if c.StorageSizeMB != 10240 {
		t.Errorf("StorageSizeMB = %d, want 10240", c.StorageSizeMB)
	}
	if c.StorageType != "custom-zvol" {
		t.Errorf("StorageType = %q, want %q", c.StorageType, "custom-zvol")
	}
	if c.StorageEmulationType != "ahci-hd" {
		t.Errorf("StorageEmulationType = %q, want %q", c.StorageEmulationType, "ahci-hd")
	}
	if c.SwitchEmulationType != "virtio" {
		t.Errorf("SwitchEmulationType = %q, want %q", c.SwitchEmulationType, "virtio")
	}
	if c.VNCPortMin != 5910 {
		t.Errorf("VNCPortMin = %d, want 5910", c.VNCPortMin)
	}
	if c.VNCPortMax != 5920 {
		t.Errorf("VNCPortMax = %d, want 5920", c.VNCPortMax)
	}
	if c.VNCResolution != "1920x1080" {
		t.Errorf("VNCResolution = %q, want %q", c.VNCResolution, "1920x1080")
	}
	if c.Loader != "bios" {
		t.Errorf("Loader = %q, want %q", c.Loader, "bios")
	}
	if c.TimeOffset != "localtime" {
		t.Errorf("TimeOffset = %q, want %q", c.TimeOffset, "localtime")
	}
	if c.ShutdownCommand != "/usr/sbin/poweroff" {
		t.Errorf("ShutdownCommand = %q, want %q", c.ShutdownCommand, "/usr/sbin/poweroff")
	}
}

// ---------------------------------------------------------------------------
// TLSSkipVerify explicit false is overridden to true
// ---------------------------------------------------------------------------

func TestConfig_TLSSkipVerify_ExplicitFalse_OverriddenToTrue(t *testing.T) {
	raw := minimalValid()
	raw["tls_skip_verify"] = false
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.TLSSkipVerify {
		t.Error("TLSSkipVerify should always be true (overridden from false)")
	}
}

func TestConfig_Prepare_DecodeError(t *testing.T) {
	c := &Config{}
	_, _, err := c.Prepare(42)
	if err == nil {
		t.Fatal("expected decode error for non-map raw input")
	}
}

func TestConfig_Defaults_VNCHost_InvalidSylveURL(t *testing.T) {
	t.Setenv("SYLVE_HOST", "")
	raw := minimalValid()
	raw["sylve_url"] = "not-a-url"
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if c.VNCHost != "127.0.0.1" {
		t.Fatalf("VNCHost = %q, want 127.0.0.1", c.VNCHost)
	}
}

func TestConfig_Prepare_MissingSSHUsername(t *testing.T) {
	raw := minimalValid()
	delete(raw, "ssh_username")
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error when ssh_username is missing")
	}
}

func TestConfig_Prepare_InvalidHTTPPort(t *testing.T) {
	raw := minimalValid()
	raw["http_port"] = -1
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for invalid http_port")
	}
}

func TestConfig_Prepare_InvalidSSHPort(t *testing.T) {
	raw := minimalValid()
	raw["ssh_port"] = "not-a-port"
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for invalid ssh_port")
	}
}

func TestConfig_Prepare_InvalidSylveAPILoginTimeout(t *testing.T) {
	raw := minimalValid()
	raw["sylve_api_login_timeout"] = "not-a-duration"
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for invalid sylve_api_login_timeout")
	}
	if !strings.Contains(err.Error(), "invalid sylve_api_login_timeout") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfig_Prepare_NegativeSylveAPILoginTimeout(t *testing.T) {
	raw := minimalValid()
	raw["sylve_api_login_timeout"] = "-1s"
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error for negative sylve_api_login_timeout")
	}
	if !strings.Contains(err.Error(), "sylve_api_login_timeout must be >= 0") {
		t.Fatalf("error = %v", err)
	}
}

func TestConfig_Prepare_SylveAPILoginTimeoutFromEnv(t *testing.T) {
	t.Setenv("SYLVE_API_LOGIN_TIMEOUT", "45s")
	t.Setenv("SYLVE_HOST", "")
	c, err := prepare(minimalValid())
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.sylveAPILoginTimeoutDur != 45*time.Second {
		t.Fatalf("sylveAPILoginTimeoutDur = %v, want 45s", c.sylveAPILoginTimeoutDur)
	}
}

// ---------------------------------------------------------------------------
// sylveHostIsLocal
// ---------------------------------------------------------------------------

func TestSylveHostIsLocal_Localhost(t *testing.T) {
	if !sylveHostIsLocal("localhost") {
		t.Error("localhost should be local")
	}
}

func TestSylveHostIsLocal_Loopback127(t *testing.T) {
	if !sylveHostIsLocal("127.0.0.1") {
		t.Error("127.0.0.1 should be local")
	}
}

func TestSylveHostIsLocal_LoopbackIPv6(t *testing.T) {
	if !sylveHostIsLocal("::1") {
		t.Error("::1 should be local")
	}
}

func TestSylveHostIsLocal_RemoteHost(t *testing.T) {
	if sylveHostIsLocal("192.0.2.1") {
		t.Error("192.0.2.1 (TEST-NET) should not be local")
	}
}

func TestSylveHostIsLocal_UnresolvableHost(t *testing.T) {
	if sylveHostIsLocal("this.host.does.not.exist.invalid") {
		t.Error("unresolvable host should not be local")
	}
}

func TestSylveHostIsLocal_IPAssignedToLocalInterface(t *testing.T) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatalf("InterfaceAddrs: %v", err)
	}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			if !sylveHostIsLocal(ip4.String()) {
				t.Fatalf("sylveHostIsLocal(%s) = false, want true", ip4)
			}
			return
		}
	}
	t.Skip("no non-loopback IPv4 address found on interfaces")
}

// ---------------------------------------------------------------------------
// SSH bastion auto-config
// ---------------------------------------------------------------------------

func TestConfig_SSHProxy_NotApplied_WhenLocalhost(t *testing.T) {
	// Sylve URL pointing at localhost: plugin is on the Sylve host, no bastion.
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	raw := map[string]interface{}{
		"sylve_url":        "https://localhost:8181",
		"sylve_user":       "admin",
		"sylve_password":   "secret",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "" {
		t.Errorf("SSHBastionHost = %q, want empty (local Sylve host)", c.Config.SSHBastionHost)
	}
}

func TestConfig_SSHProxy_Applied_WhenRemoteHost(t *testing.T) {
	// Sylve URL pointing at a non-local IP: bastion should be configured.
	// With no ~/.ssh/config entry for the host the username falls to $USER
	// and auth falls to the SSH agent. SylveUser/SylvePassword are NOT used
	// for the bastion — they are Sylve API credentials, not SSH system credentials.
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	t.Setenv("USER", "testuser")
	// Point HOME at an empty dir so sshConfigForHost finds no ~/.ssh/config.
	t.Setenv("HOME", t.TempDir())
	raw := map[string]interface{}{
		"sylve_url":        "https://192.0.2.1:8181",
		"sylve_user":       "admin",
		"sylve_password":   "secret",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "192.0.2.1" {
		t.Errorf("SSHBastionHost = %q, want %q", c.Config.SSHBastionHost, "192.0.2.1")
	}
	if c.Config.SSHBastionUsername != "testuser" {
		t.Errorf("SSHBastionUsername = %q, want %q (from $USER)", c.Config.SSHBastionUsername, "testuser")
	}
	// SylvePassword must NOT be forwarded to the bastion.
	if c.Config.SSHBastionPassword != "" {
		t.Errorf("SSHBastionPassword = %q, want empty (Sylve API password not used for SSH bastion)", c.Config.SSHBastionPassword)
	}
	// No key or password available — should fall back to SSH agent.
	if !c.Config.SSHBastionAgentAuth {
		t.Error("SSHBastionAgentAuth = false, want true (no key/password; fall back to agent)")
	}
}

func TestConfig_SSHProxy_UsesSYLVE_SSH_PROXY_KEY(t *testing.T) {
	t.Setenv("USER", "testuser")
	home := t.TempDir()
	t.Setenv("HOME", home)
	keyPath := filepath.Join(home, "bastion-from-env.pem")
	generateTestPrivateKey(t, keyPath)
	t.Setenv("SYLVE_SSH_PROXY_KEY", keyPath)
	raw := map[string]interface{}{
		"sylve_url":        "https://192.0.2.1:8181",
		"sylve_user":       "admin",
		"sylve_password":   "secret",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
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

func TestConfig_SSHProxy_UsesAgentAuth_WhenTokenOnly(t *testing.T) {
	// Token-only auth: no password to forward to bastion.
	// Bastion is still configured; SSH agent auth is used instead.
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")
	// Point HOME at an empty dir so default key probing finds nothing.
	t.Setenv("HOME", t.TempDir())
	raw := map[string]interface{}{
		"sylve_url":        "https://192.0.2.1:8181",
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "192.0.2.1" {
		t.Errorf("SSHBastionHost = %q, want %q", c.Config.SSHBastionHost, "192.0.2.1")
	}
	if !c.Config.SSHBastionAgentAuth {
		t.Error("SSHBastionAgentAuth = false, want true (no password/key available, fall back to agent)")
	}
	if c.Config.SSHBastionPassword != "" {
		t.Errorf("SSHBastionPassword = %q, want empty", c.Config.SSHBastionPassword)
	}
}

func TestConfig_SSHProxy_UsesDefaultKey_WhenNoConfigAndNoEnv(t *testing.T) {
	// When ~/.ssh/config has no IdentityFile and SYLVE_SSH_PROXY_KEY is unset,
	// the plugin must probe OpenSSH default key paths (~/.ssh/id_ed25519 etc.)
	// before falling back to agent auth.
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	t.Setenv("USER", "testuser")

	// Create a temp HOME with an empty .ssh/config (no IdentityFile) but with
	// a valid id_ed25519 key file present.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host 192.0.2.1\n\tUser testuser\n"), 0600); err != nil {
		t.Fatalf("write ssh config: %v", err)
	}
	expectedKey := filepath.Join(sshDir, "id_ed25519")
	generateTestPrivateKey(t, expectedKey)

	raw := map[string]interface{}{
		"sylve_url":        "https://192.0.2.1:8181",
		"sylve_user":       "admin",
		"sylve_password":   "secret",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionPrivateKeyFile != expectedKey {
		t.Errorf("SSHBastionPrivateKeyFile = %q, want %q (default key discovery)", c.Config.SSHBastionPrivateKeyFile, expectedKey)
	}
	if c.Config.SSHBastionAgentAuth {
		t.Error("SSHBastionAgentAuth = true, want false (default key should be used)")
	}
	if c.Config.SSHBastionPassword != "" {
		t.Errorf("SSHBastionPassword = %q, want empty", c.Config.SSHBastionPassword)
	}
}

// generateTestPrivateKey writes a valid PEM-encoded EC private key to path so
// that communicator.Config.Prepare() can parse it without error.
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

func TestSshConfigForHost_FirstIdentityFileWins(t *testing.T) {
	cfg := `
Host h.example.com
  IdentityFile ~/.ssh/first
  IdentityFile ~/.ssh/second
`
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ssh", "config"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	_, keyFile, _ := sshConfigForHost("h.example.com")
	want := filepath.Join(tmp, ".ssh", "first")
	if keyFile != want {
		t.Fatalf("identityFile = %q, want %q", keyFile, want)
	}
}

func TestSshConfigForHost_ExactMatch(t *testing.T) {
	cfg := `
Host myhost.example.com
  User deploy
  IdentityFile ~/.ssh/id_ed25519
`
	// Override UserHomeDir by writing a real file and patching via the helper.
	// sshConfigForHost reads from os.UserHomeDir()/.ssh/config, so we write
	// there via a temp dir trick using os.Setenv("HOME").
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
		t.Errorf("user = %q, want %q", user, "deploy")
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
		t.Errorf("user = %q, want %q", user, "wildcard")
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
	// '[' is an invalid path.Match pattern; matching fails and the block is ignored.
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

func TestConfig_Prepare_HTTPConfigInvalidPortRange(t *testing.T) {
	raw := minimalValid()
	raw["http_port_min"] = 9999
	raw["http_port_max"] = 8000
	_, err := prepare(raw)
	if err == nil {
		t.Fatal("expected error from HTTPConfig.Prepare for invalid port range")
	}
}

func TestConfig_SSHProxy_SkipsSSHConfigWhenBastionUsernamePreset(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	raw := map[string]interface{}{
		"sylve_url":              "https://192.0.2.1:8181",
		"sylve_token":            "tok",
		"iso_download_url":       "https://example.com/os.iso",
		"switch_name":            "sw",
		"ssh_username":           "root",
		"ssh_bastion_username":   "preset-jump-user",
		"ssh_bastion_agent_auth": true,
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "192.0.2.1" {
		t.Fatalf("SSHBastionHost = %q", c.Config.SSHBastionHost)
	}
	if c.Config.SSHBastionUsername != "preset-jump-user" {
		t.Fatalf("SSHBastionUsername = %q, want preset from HCL", c.Config.SSHBastionUsername)
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

	raw := map[string]interface{}{
		"sylve_url":        "https://192.0.2.1:8181",
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionPrivateKeyFile != rsaKey {
		t.Fatalf("SSHBastionPrivateKeyFile = %q, want %q (skip bad id_ed25519)", c.Config.SSHBastionPrivateKeyFile, rsaKey)
	}
}

func TestConfig_SSHProxy_DefaultKeyProbeSkipsDirectoryNamedLikeKey(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	// First default name is id_ed25519; a directory at that path makes ReadFile fail.
	if err := os.Mkdir(filepath.Join(sshDir, "id_ed25519"), 0700); err != nil {
		t.Fatal(err)
	}
	rsaKey := filepath.Join(sshDir, "id_rsa")
	generateTestPrivateKey(t, rsaKey)
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmp)

	raw := map[string]interface{}{
		"sylve_url":        "https://192.0.2.1:8181",
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionPrivateKeyFile != rsaKey {
		t.Fatalf("SSHBastionPrivateKeyFile = %q, want %q (skip unusable id_ed25519 path)", c.Config.SSHBastionPrivateKeyFile, rsaKey)
	}
}

func TestConfig_SSHProxy_ProxyJumpNoneDoesNotBlockPrepare(t *testing.T) {
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := "Host 192.0.2.1\n  User jumpuser\n  IdentityFile ~/.ssh/id_ed25519\n  ProxyJump none\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	generateTestPrivateKey(t, keyPath)
	t.Setenv("HOME", tmp)

	raw := map[string]interface{}{
		"sylve_url":        "https://192.0.2.1:8181",
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionUsername != "jumpuser" {
		t.Fatalf("SSHBastionUsername = %q", c.Config.SSHBastionUsername)
	}
}

func TestConfig_VNCHost_FallbackWhenSylveURLDoesNotParse(t *testing.T) {
	t.Setenv("SYLVE_HOST", "")
	c, err := prepare(map[string]interface{}{
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
		"sylve_url":        "://invalid-url",
	})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.VNCHost != "127.0.0.1" {
		t.Fatalf("VNCHost = %q, want 127.0.0.1 when Sylve URL cannot be parsed", c.VNCHost)
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
		t.Errorf("user = %q, want %q", user, "palltimo")
	}
	wantKey := filepath.Join(tmp, ".ssh", "id_ed25519")
	if keyFile != wantKey {
		t.Errorf("identityFile = %q, want %q", keyFile, wantKey)
	}
	if proxyJump != "jumpbox.example.com" {
		t.Errorf("proxyJump = %q, want %q", proxyJump, "jumpbox.example.com")
	}
}

func TestConfig_SSHProxy_UsesSSHConfigIdentityFile(t *testing.T) {
	// When ~/.ssh/config has an IdentityFile for the Sylve host, use it.
	t.Setenv("SYLVE_SSH_PROXY_KEY", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")

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

	raw := map[string]interface{}{
		"sylve_url":        "https://192.0.2.1:8181",
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	}
	c, err := prepare(raw)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.Config.SSHBastionHost != "192.0.2.1" {
		t.Errorf("SSHBastionHost = %q, want %q", c.Config.SSHBastionHost, "192.0.2.1")
	}
	if c.Config.SSHBastionUsername != "sshconfiguser" {
		t.Errorf("SSHBastionUsername = %q, want %q", c.Config.SSHBastionUsername, "sshconfiguser")
	}
	if c.Config.SSHBastionPrivateKeyFile != keyPath {
		t.Errorf("SSHBastionPrivateKeyFile = %q, want %q", c.Config.SSHBastionPrivateKeyFile, keyPath)
	}
	if c.Config.SSHBastionAgentAuth {
		t.Error("SSHBastionAgentAuth = true, want false (key file from SSH config should be used)")
	}
}
