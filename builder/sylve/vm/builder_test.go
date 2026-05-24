// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"testing"
)

func TestBuilder_ConfigSpec_NotNil(t *testing.T) {
	b := &Builder{}
	if b.ConfigSpec() == nil {
		t.Error("ConfigSpec() returned nil")
	}
}

// TestBuilder_Prepare_RequiresVMName verifies that Prepare rejects a config with
// no vm_name — the builder requires a name for the created VM.
func TestBuilder_Prepare_RequiresVMName(t *testing.T) {
	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"source_template": "base",
		"sylve_token":     "tok",
		"communicator":    "none",
	})
	if err == nil {
		t.Fatal("Prepare() expected error when vm_name is missing, got nil")
	}
}

// TestBuilder_Prepare_ValidMinimal verifies that Prepare succeeds when the
// minimum required fields are set.
func TestBuilder_Prepare_ValidMinimal(t *testing.T) {
	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"vm_name":         "my-vm",
		"source_template": "base-template",
		"sylve_token":     "tok",
		"communicator":    "ssh",
		"ssh_username":    "admin",
		"ssh_password":    "pass",
	})
	if err != nil {
		t.Fatalf("Prepare() returned unexpected error: %v", err)
	}
}

func TestBuilder_Prepare_InvalidBootWait(t *testing.T) {
	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"vm_name":         "vm1",
		"source_template": "base",
		"sylve_token":     "tok",
		"communicator":    "ssh",
		"ssh_username":    "root",
		"boot_wait":       "forty-two-bats",
	})
	if err == nil {
		t.Fatal("expected Prepare error for invalid boot_wait")
	}
}

func TestBuilder_Prepare_SYLVE_HOST_BuildsDefaultURL(t *testing.T) {
	t.Setenv("SYLVE_HOST", "sylve.test-host.example")
	t.Setenv("SYLVE_TOKEN", "from-env-token")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"vm_name":         "vm1",
		"source_template": "base",
		"communicator":    "ssh",
		"ssh_username":    "u",
		"ssh_password":    "p",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if b.config.SylveURL != "https://sylve.test-host.example:8181" {
		t.Fatalf("SylveURL = %q, want default from SYLVE_HOST", b.config.SylveURL)
	}
	if b.config.SylveToken != "from-env-token" {
		t.Fatalf("SylveToken = %q, want token from environment", b.config.SylveToken)
	}
}

func TestBuilderID_Value(t *testing.T) {
	if BuilderID != "xoro.sylvevm" {
		t.Errorf("BuilderID = %q, want %q", BuilderID, "xoro.sylvevm")
	}
}

func TestConfig_Prepare_MalformedSylveURL_SkipsAutoBastion(t *testing.T) {
	t.Setenv("SYLVE_TOKEN", "tok")
	c := &Config{}
	_, _, err := c.Prepare(map[string]interface{}{
		"vm_name":         "vm1",
		"source_template": "base",
		"sylve_url":       "http://%ZZZ", // url.Parse error; bastion block must not panic
		"communicator":    "ssh",
		"ssh_username":    "root",
	})
	if err != nil {
		t.Fatalf("unexpected Prepare error: %v", err)
	}
}
