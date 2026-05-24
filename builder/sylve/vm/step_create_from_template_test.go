// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func TestStepCreateFromTemplate_Success(t *testing.T) {
	const rid = uint(3)
	const vmID = uint(42)

	mux := http.NewServeMux()

	// ListTemplatesSimple — return one template.
	mux.HandleFunc("/api/vm/templates/simple", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[[]client.SimpleTemplate]{
			Status: "success",
			Data:   []client.SimpleTemplate{{ID: 7, Name: "base-template", SourceVMName: "base-vm"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// ListVMsSimple — RIDs 1,2 in use so next free is 3.
	mux.HandleFunc("/api/vm/simple", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			// This is GetSimpleVMByRID — won't be called in this path.
			http.NotFound(w, r)
			return
		}
		resp := client.APIResponse[[]client.SimpleVM]{
			Status: "success",
			Data: []client.SimpleVM{
				{RID: 1, State: client.DomainStateRunning},
				{RID: 2, State: client.DomainStateShutoff},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// CreateVMFromTemplate
	mux.HandleFunc("/api/vm/templates/create/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// GetVMByRID (waitForVM)
	macID := uint(99)
	mux.HandleFunc("/api/vm/3", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.VM]{
			Status: "success",
			Data: client.VM{
				ID:  vmID,
				RID: rid,
				Networks: []client.VMNetwork{
					{
						ID:         10,
						MacID:      &macID,
						Emulation:  "virtio-net",
						SwitchName: "bridge0",
						MacObj: &client.VMNetworkObject{
							Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}},
						},
					},
				},
				Storages: []client.VMStorage{{ID: 1, Type: "zvol", Name: "disk0"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// HasActiveLifecycleTask — no active task.
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/42", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:       srv.URL,
		TLSSkipVerify:  true,
		SourceTemplate: "base-template",
		VMName:         "test-vm",
	}

	state := newTestState(t)
	step := &StepCreateFromTemplate{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}

	if got, _ := state.Get("vm_rid").(uint); got != rid {
		t.Errorf("vm_rid = %d, want %d", got, rid)
	}
	if got, _ := state.Get("vm_id").(uint); got != vmID {
		t.Errorf("vm_id = %d, want %d", got, vmID)
	}
	if got, _ := state.Get("vm_mac").(string); got != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("vm_mac = %q, want %q", got, "aa:bb:cc:dd:ee:ff")
	}
	if got, _ := state.Get("vm_network_id").(uint); got != 10 {
		t.Errorf("vm_network_id = %d, want 10", got)
	}
	if got, _ := state.Get("vm_network_mac_id").(uint); got != 99 {
		t.Errorf("vm_network_mac_id = %d, want 99", got)
	}
}

func TestStepCreateFromTemplate_TemplateNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/templates/simple", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[[]client.SimpleTemplate]{
			Status: "success",
			Data:   []client.SimpleTemplate{{ID: 1, Name: "other"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:       srv.URL,
		TLSSkipVerify:  true,
		SourceTemplate: "does-not-exist",
		VMName:         "vm",
	}
	state := newTestState(t)
	step := &StepCreateFromTemplate{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when template not found")
	}
}

func TestStepCreateFromTemplate_RIDAllocationFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/templates/simple", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[[]client.SimpleTemplate]{
			Status: "success",
			Data:   []client.SimpleTemplate{{ID: 1, Name: "tpl"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// ListVMsSimple returns an error.
	mux.HandleFunc("/api/vm/simple", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:       srv.URL,
		TLSSkipVerify:  true,
		SourceTemplate: "tpl",
		VMName:         "vm",
	}
	state := newTestState(t)
	step := &StepCreateFromTemplate{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt on RID allocation failure")
	}
}

func TestStepCreateFromTemplate_CreateError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/templates/simple", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[[]client.SimpleTemplate]{
			Status: "success",
			Data:   []client.SimpleTemplate{{ID: 5, Name: "tpl"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[[]client.SimpleVM]{Status: "success", Data: []client.SimpleVM{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/templates/create/5", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:       srv.URL,
		TLSSkipVerify:  true,
		SourceTemplate: "tpl",
		VMName:         "vm",
	}
	state := newTestState(t)
	step := &StepCreateFromTemplate{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt on CreateVMFromTemplate error")
	}
}

func TestStepCreateFromTemplate_WaitForVM_Timeout(t *testing.T) {
	origPoll := createFromTemplatePollInterval
	origMax := createFromTemplateMaxWait
	createFromTemplatePollInterval = 5 * time.Millisecond
	createFromTemplateMaxWait = 30 * time.Millisecond
	t.Cleanup(func() {
		createFromTemplatePollInterval = origPoll
		createFromTemplateMaxWait = origMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/templates/simple", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[[]client.SimpleTemplate]{
			Status: "success",
			Data:   []client.SimpleTemplate{{ID: 5, Name: "tpl"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[[]client.SimpleVM]{Status: "success", Data: []client.SimpleVM{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/templates/create/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// GetVMByRID always returns 404.
	mux.HandleFunc("/api/vm/1", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:       srv.URL,
		TLSSkipVerify:  true,
		SourceTemplate: "tpl",
		VMName:         "vm",
	}

	state := newTestState(t)
	step := &StepCreateFromTemplate{Config: cfg}
	action := step.Run(context.Background(), state)
	// The step should halt because waitForVM returns a timeout error.
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}
}

func TestStepCreateFromTemplate_Cleanup_Halted(t *testing.T) {
	var deleted atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted.Store(true)
			resp := map[string]interface{}{"status": "success"}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put(multistep.StateHalted, true)

	step := &StepCreateFromTemplate{Config: cfg}
	step.Cleanup(state)

	if !deleted.Load() {
		t.Fatal("expected VM to be deleted on halted cleanup")
	}
}

func TestStepCreateFromTemplate_Cleanup_NotHalted(t *testing.T) {
	var deleted atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	// No StateHalted or StateCancelled — cleanup should be a no-op.

	step := &StepCreateFromTemplate{Config: cfg}
	step.Cleanup(state)

	if deleted.Load() {
		t.Fatal("cleanup should not delete VM when build succeeded")
	}
}

func TestStepCreateFromTemplate_Cleanup_NoRID(t *testing.T) {
	cfg := &Config{SylveURL: "http://localhost", TLSSkipVerify: true}
	state := newTestState(t)
	state.Put(multistep.StateHalted, true)
	// No vm_rid set — cleanup should not panic.

	step := &StepCreateFromTemplate{Config: cfg}
	step.Cleanup(state)
}

func TestStepCreateFromTemplate_WaitForVM_LifecycleTaskActive(t *testing.T) {
	// Tests the path where the VM exists but lifecycle task is still active,
	// then completes on the next poll.
	origPoll := createFromTemplatePollInterval
	origMax := createFromTemplateMaxWait
	createFromTemplatePollInterval = 5 * time.Millisecond
	createFromTemplateMaxWait = 200 * time.Millisecond
	t.Cleanup(func() {
		createFromTemplatePollInterval = origPoll
		createFromTemplateMaxWait = origMax
	})

	var taskPollCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/templates/simple", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[[]client.SimpleTemplate]{
			Status: "success",
			Data:   []client.SimpleTemplate{{ID: 5, Name: "tpl"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[[]client.SimpleVM]{Status: "success", Data: []client.SimpleVM{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/templates/create/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// GetVMByRID always returns the VM.
	mux.HandleFunc("/api/vm/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			return
		}
		resp := client.APIResponse[client.VM]{
			Status: "success",
			Data: client.VM{
				ID: 42, RID: 1, Name: "test-vm",
				Networks: []client.VMNetwork{{
					ID: 10, Emulation: "virtio-net",
					MacObj: &client.VMNetworkObject{
						Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}},
					},
				}},
				Storages: []client.VMStorage{{ID: 1}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// HasActiveLifecycleTask — first call returns active, then returns error, then inactive.
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/42", func(w http.ResponseWriter, _ *http.Request) {
		count := taskPollCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case count == 1:
			// Active task.
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"data":   map[string]interface{}{"id": 1},
			})
		case count == 2:
			// Non-retriable error response (500 is not in the retry list).
			http.Error(w, "internal", http.StatusInternalServerError)
		default:
			// No active task (nil data).
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
				"data":   nil,
			})
		}
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:       srv.URL,
		TLSSkipVerify:  true,
		SourceTemplate: "tpl",
		VMName:         "test-vm",
	}
	state := newTestState(t)
	step := &StepCreateFromTemplate{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
}

func TestStepCreateFromTemplate_Cleanup_DeleteError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put(multistep.StateCancelled, true)

	step := &StepCreateFromTemplate{Config: cfg}
	// Should not panic — just logs the error.
	step.Cleanup(state)
}
