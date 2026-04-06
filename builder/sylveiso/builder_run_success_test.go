// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packer "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// stubArtifactStep populates the state bag so buildArtifact can succeed without
// hitting the real Sylve API. Used only via isoBuildStepsHook under tests.
type stubArtifactStep struct{}

func (stubArtifactStep) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	state.Put("vm_id", uint(10))
	state.Put("vm_rid", uint(20))
	state.Put("generated_data", map[string]string{"k": "v"})
	return multistep.ActionContinue
}

func (stubArtifactStep) Cleanup(multistep.StateBag) {}

func TestBuilder_InstanceIPFromState(t *testing.T) {
	b := &Builder{}
	if _, err := b.instanceIPFromState(new(multistep.BasicStateBag)); err == nil {
		t.Fatal("expected error when instance_ip missing")
	}
	state := new(multistep.BasicStateBag)
	state.Put("instance_ip", "10.0.0.42")
	ip, err := b.instanceIPFromState(state)
	if err != nil || ip != "10.0.0.42" {
		t.Fatalf("ip=%q err=%v", ip, err)
	}
}

func TestBuilder_Run_ReturnsArtifactWithStubSteps(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/basic/settings" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.BasicSettings]{
				Status: "ok",
				Data:   client.BasicSettings{Initialized: true, Pools: []string{"tank"}},
			})
		case r.URL.Path == "/api/network/switch" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SwitchList]{
				Status: "ok",
				Data: client.SwitchList{
					Standard: []client.StandardSwitch{{ID: 1, Name: "sw"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_url":        srv.URL,
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	art, err := b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	a, ok := art.(*Artifact)
	if !ok {
		t.Fatalf("artifact type %T", art)
	}
	if a.VMID != 10 || a.VMRID != 20 {
		t.Fatalf("VMID=%d VMRID=%d", a.VMID, a.VMRID)
	}
}

func TestBuilder_Run_LoginLogoutWithStubSteps(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	var logoutCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "session-token"},
			})
		case r.URL.Path == "/api/auth/logout" && r.Method == http.MethodPost:
			logoutCalls++
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/basic/settings" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.BasicSettings]{
				Status: "ok",
				Data:   client.BasicSettings{Initialized: true, Pools: []string{"tank"}},
			})
		case r.URL.Path == "/api/network/switch" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SwitchList]{
				Status: "ok",
				Data: client.SwitchList{
					Standard: []client.StandardSwitch{{ID: 1, Name: "sw"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_url":        srv.URL,
		"sylve_user":       "u",
		"sylve_password":   "p",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if _, err := b.Run(context.Background(), newMockUI(), &packer.MockHook{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if logoutCalls != 1 {
		t.Fatalf("logout calls = %d, want 1", logoutCalls)
	}
}
