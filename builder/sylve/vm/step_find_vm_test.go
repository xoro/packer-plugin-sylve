// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

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

// serveFindVM launches a two-route fake Sylve API:
//   - GET /api/vm/simple      → simpleBody (simpleStatus)
//   - GET /api/vm/<rid>?type=rid → fullBody (200)
//
// FindVMByName calls both in sequence.
func serveFindVM(t *testing.T, simpleBody string, simpleStatus int, fullBody string) (*Config, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(simpleStatus)
		_, _ = w.Write([]byte(simpleBody))
	})
	// Serve any /api/vm/<rid> path.
	mux.HandleFunc("/api/vm/", func(w http.ResponseWriter, _ *http.Request) {
		if fullBody == "" {
			http.NotFound(w, nil)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fullBody))
	})
	srv := httptest.NewServer(mux)
	cfg := &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}
	return cfg, srv.Close
}

func runFindVM(t *testing.T, cfg *Config) (multistep.StepAction, multistep.StateBag) {
	t.Helper()
	ui := packersdk.TestUi(t)
	state := new(multistep.BasicStateBag)
	state.Put("ui", ui)

	step := &StepFindVM{Config: cfg}
	action := step.Run(context.Background(), state)
	return action, state
}

// buildSimpleListJSON returns the JSON for GET /api/vm/simple with one entry.
func buildSimpleListJSON(rid, id uint, name string, domState client.DomainState) string {
	type entry struct {
		RID   uint               `json:"rid"`
		ID    uint               `json:"id"`
		Name  string             `json:"name"`
		State client.DomainState `json:"state"`
	}
	list := []entry{{RID: rid, ID: id, Name: name, State: domState}}
	data, _ := json.Marshal(map[string]interface{}{
		"status": "ok",
		"data":   list,
	})
	return string(data)
}

// buildFullVMJSON returns the JSON for GET /api/vm/<rid>.
func buildFullVMJSON(rid, id uint, name string, domState client.DomainState) string {
	vm := map[string]interface{}{
		"rid":   rid,
		"id":    id,
		"name":  name,
		"state": int(domState),
	}
	data, _ := json.Marshal(map[string]interface{}{
		"status": "ok",
		"data":   vm,
	})
	return string(data)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestStepFindVM_Success(t *testing.T) {
	simpleBody := buildSimpleListJSON(7, 42, "my-vm", client.DomainStateShutoff)
	fullBody := buildFullVMJSON(7, 42, "my-vm", client.DomainStateShutoff)
	cfg, cleanup := serveFindVM(t, simpleBody, http.StatusOK, fullBody)
	defer cleanup()
	cfg.VMName = "my-vm"

	action, state := runFindVM(t, cfg)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	if rid, _ := state.Get("vm_rid").(uint); rid != 7 {
		t.Errorf("vm_rid = %d, want 7", rid)
	}
	if id, _ := state.Get("vm_id").(uint); id != 42 {
		t.Errorf("vm_id = %d, want 42", id)
	}
}

func TestStepFindVM_Success_EmptyNetworks(t *testing.T) {
	simpleBody := buildSimpleListJSON(8, 40, "no-net-vm", client.DomainStateShutoff)
	// Omit "networks" so StepFindVM stores an empty vm_mac (valid API shape).
	fullBody := buildFullVMJSON(8, 40, "no-net-vm", client.DomainStateShutoff)
	cfg, cleanup := serveFindVM(t, simpleBody, http.StatusOK, fullBody)
	defer cleanup()
	cfg.VMName = "no-net-vm"

	action, state := runFindVM(t, cfg)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	mac, _ := state.Get("vm_mac").(string)
	if mac != "" {
		t.Fatalf("expected empty MAC when VM has no network interfaces; got %q", mac)
	}
}

func TestStepFindVM_NotFound(t *testing.T) {
	// Empty list — VM does not exist.
	emptyBody := `{"status":"ok","data":[]}`
	cfg, cleanup := serveFindVM(t, emptyBody, http.StatusOK, "")
	defer cleanup()
	cfg.VMName = "missing-vm"

	action, state := runFindVM(t, cfg)

	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt for missing VM, got %v", action)
	}
	if state.Get("error") == nil {
		t.Error("expected error in state, got nil")
	}
}

func TestStepFindVM_AlreadyRunning(t *testing.T) {
	simpleBody := buildSimpleListJSON(3, 10, "running-vm", client.DomainStateRunning)
	fullBody := buildFullVMJSON(3, 10, "running-vm", client.DomainStateRunning)
	cfg, cleanup := serveFindVM(t, simpleBody, http.StatusOK, fullBody)
	defer cleanup()
	cfg.VMName = "running-vm"

	action, state := runFindVM(t, cfg)

	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt for already-running VM, got %v", action)
	}
	if state.Get("error") == nil {
		t.Error("expected error in state, got nil")
	}
}

func TestStepFindVM_APIError(t *testing.T) {
	cfg, cleanup := serveFindVM(t, `{"status":"error"}`, http.StatusInternalServerError, "")
	defer cleanup()
	cfg.VMName = "any-vm"

	action, state := runFindVM(t, cfg)

	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt for API error, got %v", action)
	}
	if state.Get("error") == nil {
		t.Error("expected error in state, got nil")
	}
}

func TestStepFindVM_Success_WithNetworkMacObj(t *testing.T) {
	simpleBody := buildSimpleListJSON(30, 99, "mac-vm", client.DomainStateShutoff)
	const fullBody = `{"status":"ok","data":{"rid":30,"id":99,"name":"mac-vm","state":5,"networks":[{"macObj":{"entries":[{"value":"dc:dc:dc:dc:dc:dc"}]}}]}}`

	cfg, cleanup := serveFindVM(t, simpleBody, http.StatusOK, fullBody)
	defer cleanup()
	cfg.VMName = "mac-vm"

	action, state := runFindVM(t, cfg)

	if action != multistep.ActionContinue {
		t.Fatalf("want continue, got %v err=%v", action, state.Get("error"))
	}
	if mac, _ := state.Get("vm_mac").(string); mac != "dc:dc:dc:dc:dc:dc" {
		t.Fatalf("vm_mac=%q", mac)
	}
}

func TestStepFindVM_Cleanup_NoOp(t *testing.T) {
	(&StepFindVM{Config: &Config{}}).Cleanup(newTestState(t))
}
