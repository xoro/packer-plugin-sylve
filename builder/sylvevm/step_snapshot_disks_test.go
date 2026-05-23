// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylvevm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// serveSnapshot starts a fake Sylve API server for StepSnapshotDisks tests.
// routes maps "METHOD /path" → handler (same pattern as zfs_test.go).
func serveSnapshot(t *testing.T, routes map[string]http.HandlerFunc) (*Config, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if h, ok := routes[key]; ok {
			h(w, r)
			return
		}
		// Default: strip query string and try again.
		if h, ok := routes[r.Method+" "+r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	cfg := &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true, PreserveOriginal: true}
	return cfg, srv.Close
}

func runSnapshot(t *testing.T, cfg *Config, storages []client.VMStorage) (multistep.StepAction, multistep.StateBag) {
	t.Helper()
	ui := packersdk.TestUi(t)
	state := new(multistep.BasicStateBag)
	state.Put("ui", ui)
	state.Put("vm_storages", storages)

	step := &StepSnapshotDisks{Config: cfg}
	action := step.Run(context.Background(), state)
	return action, state
}

// jsonOK writes a JSON "ok" envelope with the given data.
func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "data": data}) //nolint:errcheck
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestStepSnapshotDisks_Disabled(t *testing.T) {
	// preserve_original = false → no API calls, step continues immediately.
	cfg := &Config{SylveURL: "http://127.0.0.1:0", SylveToken: "tok", PreserveOriginal: false}
	storages := []client.VMStorage{
		{Type: client.VMStorageTypeZVol, Dataset: &client.VMStorageDataset{GUID: "g1", Name: "pool/vol"}},
	}

	action, state := runSnapshot(t, cfg, storages)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue when disabled, got %v", action)
	}
	if _, ok := state.GetOk("snapshot_guids"); ok {
		t.Error("snapshot_guids should not be set when preserve_original is false")
	}
}

func TestStepSnapshotDisks_Success(t *testing.T) {
	// POST snapshot succeeds; GET datasets returns a snapshot entry whose name
	// matches the pattern dsName + "@packer-pre-build-<unix-ts>". The fake server
	// cannot predict the exact timestamp so it returns a wildcard-friendly entry;
	// TakeDatasetSnapshot matches by prefix (dsName + "@" + label) in the real
	// list, so we need to serve a name that includes the actual label.
	//
	// Strategy: the fake GET /zfs/datasets handler captures the first POST body
	// to learn the label, then returns a dataset entry whose name is
	// dsName + "@" + label.
	var capturedLabel string
	routes := map[string]http.HandlerFunc{
		"POST /api/zfs/datasets/snapshot": func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			capturedLabel = body.Name
			jsonOK(w, nil)
		},
		"GET /api/zfs/datasets": func(w http.ResponseWriter, _ *http.Request) {
			snapshotName := "tank/vm-vol@" + capturedLabel
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
				"status": "ok",
				"data": []map[string]string{
					{"guid": "snap-guid-1", "name": snapshotName},
				},
			})
		},
	}
	cfg, cleanup := serveSnapshot(t, routes)
	defer cleanup()

	storages := []client.VMStorage{
		{
			Type:    client.VMStorageTypeZVol,
			Name:    "vm-vol",
			Dataset: &client.VMStorageDataset{GUID: "ds-guid-1", Name: "tank/vm-vol"},
		},
	}

	action, state := runSnapshot(t, cfg, storages)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	guids, _ := state.Get("snapshot_guids").(map[string]string)
	if len(guids) != 1 {
		t.Fatalf("snapshot_guids len = %d, want 1", len(guids))
	}
}

func TestStepSnapshotDisks_SkipsNonZVOL(t *testing.T) {
	// Storages without zvol/filesystem type — no API call, continues cleanly.
	cfg := &Config{SylveURL: "http://127.0.0.1:0", SylveToken: "tok", PreserveOriginal: true}
	storages := []client.VMStorage{
		{Type: client.VMStorageTypeDiskImage, Name: "img"},
		{Type: client.VMStorageTypeRaw, Name: "raw"},
	}

	// Run directly; any outbound API call would fail because port 0 is not bound.
	ui := packersdk.TestUi(t)
	state := new(multistep.BasicStateBag)
	state.Put("ui", ui)
	state.Put("vm_storages", storages)

	step := &StepSnapshotDisks{Config: cfg}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue when all storages are skipped, got %v", action)
	}
	guids, _ := state.Get("snapshot_guids").(map[string]string)
	if len(guids) != 0 {
		t.Errorf("expected empty snapshot_guids, got %v", guids)
	}
}

func TestStepSnapshotDisks_APIError(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"POST /api/zfs/datasets/snapshot": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"error","error":"disk busy"}`))
		},
	}
	cfg, cleanup := serveSnapshot(t, routes)
	defer cleanup()

	storages := []client.VMStorage{
		{
			Type:    client.VMStorageTypeZVol,
			Name:    "vm-vol",
			Dataset: &client.VMStorageDataset{GUID: "ds-guid-2", Name: "tank/vm-vol2"},
		},
	}

	action, state := runSnapshot(t, cfg, storages)

	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt on API error, got %v", action)
	}
	if state.Get("error") == nil {
		t.Error("expected error in state, got nil")
	}
}

func TestStepSnapshotDisks_Cleanup_Rollback(t *testing.T) {
	rollbackCalled := false
	routes := map[string]http.HandlerFunc{
		"POST /api/zfs/datasets/snapshot/rollback": func(w http.ResponseWriter, _ *http.Request) {
			rollbackCalled = true
			jsonOK(w, nil)
		},
	}
	cfg, cleanup := serveSnapshot(t, routes)
	defer cleanup()

	ui := packersdk.TestUi(t)
	state := new(multistep.BasicStateBag)
	state.Put("ui", ui)
	state.Put("snapshot_guids", map[string]string{"ds-guid": "snap-guid"})

	step := &StepSnapshotDisks{Config: cfg}
	step.Cleanup(state)

	if !rollbackCalled {
		t.Error("expected rollback API to be called during Cleanup")
	}
}
