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
)

func TestStepDeleteVM_CleanupNoOp_New(t *testing.T) {
	step := &StepDeleteVM{Config: &Config{}}
	step.Cleanup(newTestState(t))
}

func TestStepDeleteVM_Destroy_Success(t *testing.T) {
	// DELETE /api/vm/:rid?deletemacs=true&deleterawdisks=true&deletevolumes=true
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

	cfg := &Config{
		SylveURL:      srv.URL,
		TLSSkipVerify: true,
		Destroy:       true,
	}
	state := newTestState(t)
	state.Put("vm_rid", uint(11))

	step := &StepDeleteVM{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	// After deletion vm_rid should be zeroed.
	if rid, _ := state.Get("vm_rid").(uint); rid != 0 {
		t.Errorf("vm_rid = %d, want 0 after deletion", rid)
	}
}

func TestStepDeleteVM_Destroy_ZeroRID(t *testing.T) {
	// If vm_rid is 0 (not set), the step should continue without calling DeleteVM.
	cfg := &Config{Destroy: true}
	state := newTestState(t)
	// vm_rid not set — will read as zero value

	step := &StepDeleteVM{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue for zero RID, got %v", action)
	}
}
