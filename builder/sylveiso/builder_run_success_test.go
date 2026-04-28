// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

// haltErrorStep puts a fixed error in the state bag so Builder.Run surfaces it.
type haltErrorStep struct{ err error }

func (h haltErrorStep) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	state.Put("error", h.err)
	return multistep.ActionHalt
}

func (haltErrorStep) Cleanup(multistep.StateBag) {}

func newSylvePreflightServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

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

func TestBuilder_Run_WithEmptyHookStepList(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step { return nil }

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

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
	if a.VMRID != 0 || a.VMID != 0 {
		t.Fatalf("expected zero VM IDs with no steps, got VMRID=%d VMID=%d", a.VMRID, a.VMID)
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
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

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
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

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

func TestBuilder_Run_ReturnsStateBagError(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{haltErrorStep{err: errors.New("injected step failure")}}
	}

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

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

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected error from state bag")
	}
	if err.Error() != "injected step failure" {
		t.Fatalf("error = %v", err)
	}
}

func TestBuilder_Run_CheckSylveReadyFailsWhenNotInitialized(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/basic/settings" && r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.BasicSettings]{
				Status: "ok",
				Data:   client.BasicSettings{Initialized: false, Pools: []string{}},
			})
			return
		}
		http.NotFound(w, r)
	}))

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

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

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected checkSylveReady error")
	}
	if !strings.Contains(err.Error(), "has not been initialised") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuilder_Run_CheckSylveReadyFailsWhenPoolMissing(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/basic/settings" && r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.BasicSettings]{
				Status: "ok",
				Data:   client.BasicSettings{Initialized: true, Pools: []string{"tank", "zroot"}},
			})
			return
		}
		http.NotFound(w, r)
	}))

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_url":        srv.URL,
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
		"storage_pool":     "nonexistent-pool",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected pool validation error")
	}
	if !strings.Contains(err.Error(), "nonexistent-pool") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuilder_Run_EnsureSwitchFailsWhenConfiguredSwitchMissing(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					Standard: []client.StandardSwitch{{ID: 1, Name: "other-switch"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_url":        srv.URL,
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "expected-switch",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected ensureSwitch error")
	}
	if !strings.Contains(err.Error(), "expected-switch") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuilder_Run_EnsureAuthFailurePropagates(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

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

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected ensureAuth error")
	}
}

func TestBuilder_Run_BuildArtifactPrefersVmRidFinal(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubRIDFinalStep{}}
	}

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

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
	if a.VMRID != 999 {
		t.Fatalf("VMRID=%d, want 999 (vm_rid_final)", a.VMRID)
	}
}

// stubRIDFinalStep sets vm_rid_final for buildArtifact precedence tests.
type stubRIDFinalStep struct{}

func (stubRIDFinalStep) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	state.Put("vm_id", uint(1))
	state.Put("vm_rid", uint(0))
	state.Put("vm_rid_final", uint(999))
	state.Put("generated_data", map[string]string{})
	return multistep.ActionContinue
}

func (stubRIDFinalStep) Cleanup(multistep.StateBag) {}

func TestBuilder_Run_CheckSylveReadyFailsOnSettingsHTTPError(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step { return []multistep.Step{stubArtifactStep{}} }

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/basic/settings" && r.Method == http.MethodGet {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

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

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected error from GetBasicSettings")
	}
}

func TestBuilder_Run_CreateStandardSwitchFails(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				Data:   client.SwitchList{Standard: []client.StandardSwitch{}},
			})
		case r.URL.Path == "/api/network/switch/standard" && r.Method == http.MethodPost:
			http.Error(w, "cannot create", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_url":        srv.URL,
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected ensureSwitch create error")
	}
}

func TestBuilder_Run_AutoCreatesSwitchWhenNoneExist(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	var createCalls int
	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				Data:   client.SwitchList{Standard: []client.StandardSwitch{}},
			})
		case r.URL.Path == "/api/network/switch/standard" && r.Method == http.MethodPost:
			createCalls++
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_url":        srv.URL,
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if _, err := b.Run(context.Background(), newMockUI(), &packer.MockHook{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if createCalls != 1 {
		t.Fatalf("CreateStandardSwitch POST calls = %d, want 1", createCalls)
	}
}

func TestBuilder_Run_EnsureSwitchFailsOnSwitchListHTTPError(t *testing.T) {
	t.Cleanup(func() { isoBuildStepsHook = nil })
	isoBuildStepsHook = func(_ *Builder) []multistep.Step { return []multistep.Step{stubArtifactStep{}} }

	srv := newSylvePreflightServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/basic/settings" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.BasicSettings]{
				Status: "ok",
				Data:   client.BasicSettings{Initialized: true, Pools: []string{"tank"}},
			})
		case r.URL.Path == "/api/network/switch" && r.Method == http.MethodGet:
			http.Error(w, "server error", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))

	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_POOL", "")
	t.Setenv("SYLVE_SWITCH", "")

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

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected error from ListSwitches")
	}
}
