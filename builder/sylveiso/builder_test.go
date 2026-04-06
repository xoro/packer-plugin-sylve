// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packer "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// newMockUI returns a packer.MockUi, which satisfies the full packersdk.Ui interface.
func newMockUI() *packer.MockUi {
	return &packer.MockUi{}
}

// newBuilderWithURL creates a Builder whose SylveURL points at the given server.
func newBuilderWithURL(serverURL string) *Builder {
	return &Builder{config: Config{
		SylveURL:   serverURL,
		SylveToken: "tok",
	}}
}

// serveBasicSettings starts an httptest server that returns BasicSettings from
// GET /api/basic/settings.
func serveBasicSettings(t *testing.T, settings client.BasicSettings) (*Builder, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/basic/settings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.APIResponse[client.BasicSettings]{
			Status: "ok",
			Data:   settings,
		})
	}))
	b := newBuilderWithURL(srv.URL)
	return b, srv
}

// ---------------------------------------------------------------------------
// defaultISOSteps
// ---------------------------------------------------------------------------

func TestDefaultISOSteps_RestartAfterInstallAddsStep(t *testing.T) {
	b := &Builder{config: Config{}}
	nWithout := len(b.defaultISOSteps())
	b.config.RestartAfterInstall = true
	nWith := len(b.defaultISOSteps())
	if nWithout != 10 {
		t.Fatalf("len(defaultISOSteps) without restart = %d, want 10", nWithout)
	}
	if nWith != 11 {
		t.Fatalf("len(defaultISOSteps) with restart = %d, want 11", nWith)
	}
}

// ---------------------------------------------------------------------------
// switchNames
// ---------------------------------------------------------------------------

func TestSwitchNames_Empty(t *testing.T) {
	names := switchNames([]client.StandardSwitch{})
	if len(names) != 0 {
		t.Errorf("expected empty slice, got %v", names)
	}
}

func TestSwitchNames_Multiple(t *testing.T) {
	names := switchNames([]client.StandardSwitch{
		{ID: 1, Name: "vmbr0"},
		{ID: 2, Name: "packer"},
	})
	if len(names) != 2 || names[0] != "vmbr0" || names[1] != "packer" {
		t.Errorf("unexpected names: %v", names)
	}
}

// ---------------------------------------------------------------------------
// checkSylveReady
// ---------------------------------------------------------------------------

func TestCheckSylveReady_Initialized_AutoPool(t *testing.T) {
	// StoragePool not set — should be auto-set to first pool.
	b, srv := serveBasicSettings(t, client.BasicSettings{
		Initialized: true,
		Pools:       []string{"tank", "data"},
	})
	defer srv.Close()
	ui := newMockUI()

	if err := b.checkSylveReady(ui); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.config.StoragePool != "tank" {
		t.Errorf("StoragePool = %q, want %q", b.config.StoragePool, "tank")
	}
}

func TestCheckSylveReady_Initialized_ExplicitPool_Found(t *testing.T) {
	b, srv := serveBasicSettings(t, client.BasicSettings{
		Initialized: true,
		Pools:       []string{"tank", "data"},
	})
	defer srv.Close()
	b.config.StoragePool = "data"
	ui := newMockUI()

	if err := b.checkSylveReady(ui); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckSylveReady_Initialized_ExplicitPool_NotFound(t *testing.T) {
	b, srv := serveBasicSettings(t, client.BasicSettings{
		Initialized: true,
		Pools:       []string{"tank"},
	})
	defer srv.Close()
	b.config.StoragePool = "missing"
	ui := newMockUI()

	if err := b.checkSylveReady(ui); err == nil {
		t.Fatal("expected error for pool not found, got nil")
	}
}

func TestCheckSylveReady_NotInitialized(t *testing.T) {
	b, srv := serveBasicSettings(t, client.BasicSettings{
		Initialized: false,
		Pools:       []string{},
	})
	defer srv.Close()
	ui := newMockUI()

	if err := b.checkSylveReady(ui); err == nil {
		t.Fatal("expected error when Sylve is not initialized, got nil")
	}
}

func TestCheckSylveReady_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()
	b := newBuilderWithURL(srv.URL)
	ui := newMockUI()

	if err := b.checkSylveReady(ui); err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

// ---------------------------------------------------------------------------
// ensureSwitch
// ---------------------------------------------------------------------------

// serveSwitches starts a handler that responds to GET /api/network/switch and
// optionally POST /api/network/switch/standard.
func serveSwitches(t *testing.T, switches client.SwitchList, allowCreate bool) (*Builder, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/network/switch" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SwitchList]{
				Status: "ok",
				Data:   switches,
			})
		case r.URL.Path == "/api/network/switch/standard" && r.Method == http.MethodPost && allowCreate:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.Error(w, fmt.Sprintf("unexpected %s %s", r.Method, r.URL.Path), http.StatusNotFound)
		}
	}))
	b := newBuilderWithURL(srv.URL)
	return b, srv
}

func TestEnsureSwitch_ExplicitName_Found(t *testing.T) {
	b, srv := serveSwitches(t, client.SwitchList{
		Standard: []client.StandardSwitch{{ID: 1, Name: "packer-switch"}},
	}, false)
	defer srv.Close()
	b.config.SwitchName = "packer-switch"
	ui := newMockUI()

	if err := b.ensureSwitch(ui); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSwitch_ExplicitName_NotFound(t *testing.T) {
	b, srv := serveSwitches(t, client.SwitchList{
		Standard: []client.StandardSwitch{{ID: 1, Name: "vmbr0"}},
	}, false)
	defer srv.Close()
	b.config.SwitchName = "missing"
	ui := newMockUI()

	if err := b.ensureSwitch(ui); err == nil {
		t.Fatal("expected error for switch not found, got nil")
	}
}

func TestEnsureSwitch_AutoCreate_PackerSwitchAlreadyExists(t *testing.T) {
	b, srv := serveSwitches(t, client.SwitchList{
		Standard: []client.StandardSwitch{{ID: 1, Name: "Packer"}},
	}, false)
	defer srv.Close()
	ui := newMockUI()

	if err := b.ensureSwitch(ui); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.config.SwitchName != "Packer" {
		t.Errorf("SwitchName = %q, want %q", b.config.SwitchName, "Packer")
	}
}

func TestEnsureSwitch_AutoCreate_NoSwitches_Creates(t *testing.T) {
	b, srv := serveSwitches(t, client.SwitchList{
		Standard: []client.StandardSwitch{},
	}, true)
	defer srv.Close()
	ui := newMockUI()

	if err := b.ensureSwitch(ui); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.config.SwitchName != "Packer" {
		t.Errorf("SwitchName = %q, want %q", b.config.SwitchName, "Packer")
	}
}

func TestEnsureSwitch_ListSwitchesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()
	b := newBuilderWithURL(srv.URL)
	ui := newMockUI()

	if err := b.ensureSwitch(ui); err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Builder.ConfigSpec / Builder.Prepare
// ---------------------------------------------------------------------------

func TestBuilder_ConfigSpec_NotNil(t *testing.T) {
	b := &Builder{}
	if b.ConfigSpec() == nil {
		t.Error("ConfigSpec() returned nil")
	}
}

func TestBuilder_Prepare_MinimalValid(t *testing.T) {
	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")
	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare() returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ensureSwitch: create fails
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Builder.Run (early exits)
// ---------------------------------------------------------------------------

func TestBuilder_Run_LoginFails(t *testing.T) {
	t.Setenv("SYLVE_TOKEN", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_url":        srv.URL,
		"sylve_user":       "u",
		"sylve_password":   "p",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected error from Sylve login, got nil")
	}
}

func TestBuilder_Run_CheckSylveReadyFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_url":        srv.URL,
		"sylve_token":      "tok",
		"iso_download_url": "https://example.com/os.iso",
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected error from checkSylveReady, got nil")
	}
}

func TestBuilder_Run_EnsureSwitchFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/basic/settings" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.BasicSettings]{
				Status: "ok",
				Data: client.BasicSettings{
					Initialized: true,
					Pools:       []string{"tank"},
				},
			})
		case r.URL.Path == "/api/network/switch" && r.Method == http.MethodGet:
			http.Error(w, "internal error", http.StatusInternalServerError)
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
		"switch_name":      "packer-switch",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected error from ensureSwitch, got nil")
	}
}

func TestEnsureSwitch_AutoCreate_CreateFails(t *testing.T) {
	// No switches exist, but the create endpoint returns an error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/network/switch" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SwitchList]{
				Status: "ok",
				Data:   client.SwitchList{Standard: []client.StandardSwitch{}, Manual: []client.ManualSwitch{}},
			})
		default:
			http.Error(w, "conflict", http.StatusConflict)
		}
	}))
	defer srv.Close()

	b := newBuilderWithURL(srv.URL)
	ui := newMockUI()
	if err := b.ensureSwitch(ui); err == nil {
		t.Fatal("expected error when switch creation fails, got nil")
	}
}

func TestBuildArtifact(t *testing.T) {
	b := &Builder{config: Config{SylveURL: "https://h:8181", VNCPort: 5905}}
	state := new(multistep.BasicStateBag)
	state.Put("vm_id", uint(3))
	state.Put("vm_rid", uint(9))
	state.Put("generated_data", map[string]string{"k": "v"})
	art := b.buildArtifact(state)
	a, ok := art.(*Artifact)
	if !ok {
		t.Fatalf("artifact type %T", art)
	}
	if a.VMID != 3 || a.VMRID != 9 {
		t.Fatalf("VMID=%d VMRID=%d", a.VMID, a.VMRID)
	}
	if a.State("SYLVE_URL") != "https://h:8181" || a.State("SYLVE_VNC_PORT") != "5905" {
		t.Fatalf("state: %#v / %#v", a.State("SYLVE_URL"), a.State("SYLVE_VNC_PORT"))
	}
}

func TestBuilder_Run_ExistingISODownloadFailed(t *testing.T) {
	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")

	iso := "https://example.com/broken.iso"
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
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{
				Status: "ok",
				Data: []client.Download{{
					URL:    iso,
					Status: client.DownloadStatusFailed,
					Error:  "checksum",
				}},
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
		"iso_download_url": iso,
		"switch_name":      "sw",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected error from failed ISO download record, got nil")
	}
}

func TestBuilder_Run_LoginThenExistingISODownloadFailed(t *testing.T) {
	t.Setenv("SYLVE_HOST", "")
	t.Setenv("SYLVE_TOKEN", "")
	t.Setenv("SYLVE_USER", "")
	t.Setenv("SYLVE_PASSWORD", "")

	iso := "https://example.com/broken.iso"
	var logoutCalls int32
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
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{
				Status: "ok",
				Data: []client.Download{{
					URL:    iso,
					Status: client.DownloadStatusFailed,
					Error:  "checksum",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"sylve_url":        srv.URL,
		"sylve_user":       "u",
		"sylve_password":   "p",
		"iso_download_url": iso,
		"switch_name":      "sw",
		"ssh_username":     "root",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	_, err = b.Run(context.Background(), newMockUI(), &packer.MockHook{})
	if err == nil {
		t.Fatal("expected error from failed ISO download record, got nil")
	}
	if logoutCalls != 1 {
		t.Fatalf("logout calls = %d, want 1", logoutCalls)
	}
}
