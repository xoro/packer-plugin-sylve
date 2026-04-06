// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
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
