// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	sylvecommon "github.com/xoro/packer-plugin-sylve/builder/sylve/common"
)

// newTestState returns a BasicStateBag pre-populated with a no-op UI.
func newTestState(t *testing.T) *multistep.BasicStateBag {
	t.Helper()
	state := new(multistep.BasicStateBag)
	state.Put("ui", packersdk.TestUi(t))
	return state
}

// ---------------------------------------------------------------------------
// Builder helpers
// ---------------------------------------------------------------------------

func TestBuilder_buildArtifact(t *testing.T) {
	b := &Builder{}
	state := newTestState(t)
	state.Put("vm_rid", uint(9))
	state.Put("vm_id", uint(4))

	art := b.buildArtifact(state)
	if art.VMRID != 9 {
		t.Errorf("VMRID = %d, want 9", art.VMRID)
	}
	if art.VMID != 4 {
		t.Errorf("VMID = %d, want 4", art.VMID)
	}
}

func TestBuilder_instanceIPFromState_Present(t *testing.T) {
	b := &Builder{}
	state := newTestState(t)
	state.Put("instance_ip", "10.0.0.5")

	ip, err := b.instanceIPFromState(state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "10.0.0.5" {
		t.Errorf("ip = %q, want %q", ip, "10.0.0.5")
	}
}

func TestBuilder_instanceIPFromState_Missing(t *testing.T) {
	b := &Builder{}
	state := newTestState(t)

	_, err := b.instanceIPFromState(state)
	if err == nil {
		t.Error("expected error when instance_ip is not in state")
	}
}

// ---------------------------------------------------------------------------
// Config.Prepare edge cases
// ---------------------------------------------------------------------------

func TestConfig_Prepare_UserPasswordAuth(t *testing.T) {
	c := &Config{}
	_, _, err := c.Prepare(map[string]interface{}{
		"vm_name":        "my-vm",
		"sylve_user":     "admin",
		"sylve_password": "pass",
		"communicator":   "ssh",
		"ssh_username":   "root",
	})
	if err != nil {
		t.Fatalf("Prepare() failed with user+password auth: %v", err)
	}
}

func TestConfig_Prepare_MissingAuth(t *testing.T) {
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")
	c := &Config{}
	_, _, err := c.Prepare(map[string]interface{}{
		"vm_name":      "my-vm",
		"communicator": "ssh",
		"ssh_username": "root",
	})
	if err == nil {
		t.Fatal("Prepare() expected error when no auth is provided")
	}
}

func TestConfig_Prepare_InvalidLoginTimeout(t *testing.T) {
	c := &Config{}
	_, _, err := c.Prepare(map[string]interface{}{
		"vm_name":                 "my-vm",
		"sylve_token":             "tok",
		"sylve_api_login_timeout": "not-a-duration",
		"communicator":            "ssh",
		"ssh_username":            "root",
	})
	if err == nil {
		t.Fatal("Prepare() expected error for invalid duration")
	}
}

func TestConfig_Prepare_NegativeLoginTimeout(t *testing.T) {
	c := &Config{}
	_, _, err := c.Prepare(map[string]interface{}{
		"vm_name":                 "my-vm",
		"sylve_token":             "tok",
		"sylve_api_login_timeout": "-5s",
		"communicator":            "ssh",
		"ssh_username":            "root",
	})
	if err == nil {
		t.Fatal("Prepare() expected error for negative duration")
	}
}

func TestConfig_Prepare_SylveAPILoginTimeout_EnvFallback(t *testing.T) {
	t.Setenv("SYLVE_API_LOGIN_TIMEOUT", "10m")
	c := &Config{}
	_, _, err := c.Prepare(map[string]interface{}{
		"vm_name":      "my-vm",
		"sylve_token":  "tok",
		"communicator": "ssh",
		"ssh_username": "root",
	})
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	if c.sylveAPILoginTimeoutDur != 10*time.Minute {
		t.Errorf("sylveAPILoginTimeoutDur = %v, want 10m", c.sylveAPILoginTimeoutDur)
	}
}

func TestConfig_Prepare_SylveHostEnvFallback(t *testing.T) {
	t.Setenv("SYLVE_HOST", "192.168.1.10")
	c := &Config{}
	_, _, err := c.Prepare(map[string]interface{}{
		"vm_name":      "my-vm",
		"sylve_token":  "tok",
		"communicator": "ssh",
		"ssh_username": "root",
	})
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	if c.SylveURL != "https://192.168.1.10:8181" {
		t.Errorf("SylveURL = %q, want %q", c.SylveURL, "https://192.168.1.10:8181")
	}
}

func TestConfig_Prepare_DecodeError(t *testing.T) {
	c := &Config{}
	_, _, err := c.Prepare(42)
	if err == nil {
		t.Fatal("expected decode error for non-map raw input")
	}
}

func TestConfig_Prepare_DefaultSylveAuthType(t *testing.T) {
	t.Setenv("SYLVE_AUTH_TYPE", "")
	c := &Config{}
	_, _, err := c.Prepare(map[string]interface{}{
		"vm_name":      "my-vm",
		"sylve_token":  "tok",
		"communicator": "ssh",
		"ssh_username": "root",
	})
	if err != nil {
		t.Fatalf("Prepare() failed: %v", err)
	}
	if c.SylveAuthType != "sylve" {
		t.Fatalf("SylveAuthType = %q, want sylve", c.SylveAuthType)
	}
}

// ---------------------------------------------------------------------------
// StepDeleteVM
// ---------------------------------------------------------------------------

func TestStepDeleteVM_DestroyFalse(t *testing.T) {
	cfg := &Config{Destroy: false}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))

	step := &sylvecommon.StepDeleteVM{Destroy: cfg.Destroy}
	action := step.Run(context.TODO(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue when Destroy=false, got %v", action)
	}
}

// ---------------------------------------------------------------------------
// Cleanup no-op coverage (each step has func Cleanup(_ multistep.StateBag) {})
// ---------------------------------------------------------------------------

func TestAllSteps_CleanupNoOps(t *testing.T) {
	state := newTestState(t)
	cfg := &Config{}
	(&StepBootWait{Config: cfg}).Cleanup(state)
	(&sylvecommon.StepDeleteVM{}).Cleanup(state)
	(&sylvecommon.StepDiscoverIP{}).Cleanup(state)
	(&StepFindVM{Config: cfg}).Cleanup(state)
	(&StepShutdown{Config: cfg}).Cleanup(state)
	(&StepStartVM{Config: cfg}).Cleanup(state)
}
