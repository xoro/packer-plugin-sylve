// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package common

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

func TestStepDeleteVM_CleanupNoOp(t *testing.T) {
	step := &StepDeleteVM{}
	step.Cleanup(newTestState(t))
}

func TestStepDeleteVM_DestroyFalse(t *testing.T) {
	step := &StepDeleteVM{Destroy: false}
	state := newTestState(t)
	state.Put("vm_rid", uint(11))

	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue when Destroy=false, got %v", action)
	}
	// vm_rid should remain unchanged.
	if rid, _ := state.Get("vm_rid").(uint); rid != 11 {
		t.Errorf("vm_rid = %d, want 11 (unchanged)", rid)
	}
}

func TestStepDeleteVM_Destroy_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/11", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	step := &StepDeleteVM{
		SylveURL:      srv.URL,
		SylveToken:    "",
		TLSSkipVerify: true,
		Destroy:       true,
	}
	state := newTestState(t)
	state.Put("vm_rid", uint(11))

	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	if rid, _ := state.Get("vm_rid").(uint); rid != 0 {
		t.Errorf("vm_rid = %d, want 0 after deletion", rid)
	}
}

// TestStepDeleteVM_Destroy_LogsStorageInventory covers the branch where the
// pre-delete storage inventory fetch succeeds and logs each storage device,
// including one with a ZFS dataset record and one without.
func TestStepDeleteVM_Destroy_LogsStorageInventory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/12", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			resp := map[string]interface{}{
				"status": "success",
				"data": map[string]interface{}{
					"id":   100,
					"rid":  12,
					"name": "packer-test",
					"storages": []map[string]interface{}{
						{"id": 1, "type": "zvol", "name": "disk0", "pool": "tank", "dataset": map[string]interface{}{"pool": "tank", "name": "vms/x"}},
						{"id": 2, "type": "image", "name": "cd0", "pool": "tank"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": nil})
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	step := &StepDeleteVM{
		SylveURL:      srv.URL,
		TLSSkipVerify: true,
		Destroy:       true,
	}
	state := newTestState(t)
	state.Put("vm_rid", uint(12))

	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	if rid, _ := state.Get("vm_rid").(uint); rid != 0 {
		t.Errorf("vm_rid = %d, want 0 after deletion", rid)
	}
}

func TestStepDeleteVM_Destroy_ZeroRID(t *testing.T) {
	step := &StepDeleteVM{Destroy: true}
	state := newTestState(t)
	// vm_rid not set — reads as zero

	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue for zero RID, got %v", action)
	}
}

// TestStepDeleteVM_Destroy_APIError covers the error branch where DeleteVM fails.
func TestStepDeleteVM_Destroy_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/7", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	step := &StepDeleteVM{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
		Destroy:       true,
	}
	state := newTestState(t)
	state.Put("vm_rid", uint(7))

	// Error path still returns ActionContinue (deletion failure is non-fatal).
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue on delete error, got %v", action)
	}
	// vm_rid should remain set (not zeroed) since deletion failed.
	if rid, _ := state.Get("vm_rid").(uint); rid != 7 {
		t.Errorf("vm_rid = %d, want 7 (unchanged on error)", rid)
	}
}
