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

func TestStepFixNIC_CleanupNoOp(t *testing.T) {
	step := &StepFixNIC{Config: &Config{}}
	step.Cleanup(newTestState(t))
}

func TestStepFixNIC_SkipWhenNoNetID(t *testing.T) {
	cfg := &Config{SylveURL: "http://localhost", TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(1))
	// No vm_network_id → skip.

	step := &StepFixNIC{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatal("expected ActionContinue when no vm_network_id")
	}
}

func TestStepFixNIC_Success(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 10 * time.Millisecond
	fixNICBootstrapMaxWait = 200 * time.Millisecond
	fixNICStopMaxWait = 200 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	const rid = uint(5)
	var startCalled, stopCalled, detachCalled, attachCalled atomic.Bool
	var pollCount atomic.Int32

	mux := http.NewServeMux()

	// StartVM
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		startCalled.Store(true)
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// StopVM
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		stopCalled.Store(true)
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// GetSimpleVMByRID — first call returns Running (bootstrap success),
	// subsequent calls after stop return Shutoff.
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		state := client.DomainStateRunning
		if count > 1 {
			state = client.DomainStateShutoff
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: state},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// DetachVMNetwork
	mux.HandleFunc("/api/vm/network/detach", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		detachCalled.Store(true)
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// ReattachVMNetwork
	mux.HandleFunc("/api/vm/network/attach", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		attachCalled.Store(true)
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// GetVMByRID — re-fetch after reattach returns new MAC.
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.VM]{
			Status: "success",
			Data: client.VM{
				ID:  42,
				RID: rid,
				Networks: []client.VMNetwork{
					{
						ID:        20,
						Emulation: "virtio-net",
						MacObj: &client.VMNetworkObject{
							Entries: []client.VMNetworkObjectEntry{{Value: "11:22:33:44:55:66"}},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_emulation", "virtio-net")
	state.Put("vm_network_switch", "bridge0")

	step := &StepFixNIC{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	if !startCalled.Load() {
		t.Error("StartVM was not called")
	}
	if !stopCalled.Load() {
		t.Error("StopVM was not called")
	}
	if !detachCalled.Load() {
		t.Error("DetachVMNetwork was not called")
	}
	if !attachCalled.Load() {
		t.Error("ReattachVMNetwork was not called")
	}
	if mac, _ := state.Get("vm_mac").(string); mac != "11:22:33:44:55:66" {
		t.Errorf("vm_mac = %q, want %q", mac, "11:22:33:44:55:66")
	}
}

func TestStepFixNIC_NoSwitchName_Halt(t *testing.T) {
	origPoll := fixNICPollInterval
	fixNICPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { fixNICPollInterval = origPoll })

	mux := http.NewServeMux()
	// ListSwitches returns empty list.
	mux.HandleFunc("/api/network/switch", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SwitchList]{
			Status: "success",
			Data:   client.SwitchList{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	// No switch name in state and no SYLVE_SWITCH env.
	t.Setenv("SYLVE_SWITCH", "")

	step := &StepFixNIC{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when no switch available")
	}
}

func TestStepFixNIC_BootstrapStartFails_Halt(t *testing.T) {
	origPoll := fixNICPollInterval
	fixNICPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { fixNICPollInterval = origPoll })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")

	step := &StepFixNIC{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when bootstrap start fails")
	}
}

func TestStepFixNIC_StopTimeout_Halt(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 30 * time.Millisecond
	fixNICStopMaxWait = 30 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// Always return Running so stop-poll times out.
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: client.DomainStateRunning},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")

	step := &StepFixNIC{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when stop times out")
	}
}

func TestStepFixNIC_DetachError_Halt(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 50 * time.Millisecond
	fixNICStopMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	var pollCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		// First poll: Running (bootstrap domain exists), after stop: Shutoff.
		state := client.DomainStateRunning
		if count > 1 {
			state = client.DomainStateShutoff
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: state},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/detach", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")

	step := &StepFixNIC{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when detach fails")
	}
}

func TestStepFixNIC_ReattachError_Halt(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 50 * time.Millisecond
	fixNICStopMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	var pollCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		state := client.DomainStateRunning
		if count > 1 {
			state = client.DomainStateShutoff
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: state},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/detach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/attach", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")

	step := &StepFixNIC{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when reattach fails")
	}
}

func TestStepFixNIC_WinRM_PreservesMAC(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 50 * time.Millisecond
	fixNICStopMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	var pollCount atomic.Int32
	var receivedMacID *uint

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		vmState := client.DomainStateRunning
		if count > 1 {
			vmState = client.DomainStateShutoff
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: vmState},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/detach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/attach", func(w http.ResponseWriter, r *http.Request) {
		var req client.NetworkAttachRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedMacID = req.MacID
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.VM]{
			Status: "success",
			Data: client.VM{
				RID: 5,
				Networks: []client.VMNetwork{{
					ID: 20,
					MacObj: &client.VMNetworkObject{
						Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}},
					},
				}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	// Set communicator type to WinRM.
	cfg.Config.Type = "winrm"
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")
	state.Put("vm_network_mac_id", uint(77))

	step := &StepFixNIC{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	if receivedMacID == nil || *receivedMacID != 77 {
		t.Fatalf("expected macId=77 in attach request for WinRM, got %v", receivedMacID)
	}
}

func TestStepFixNIC_SwitchFromEnv(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 50 * time.Millisecond
	fixNICStopMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	t.Setenv("SYLVE_SWITCH", "env-bridge")
	var pollCount atomic.Int32
	var receivedSwitch string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		vmState := client.DomainStateRunning
		if count > 1 {
			vmState = client.DomainStateShutoff
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: vmState},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/detach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/attach", func(w http.ResponseWriter, r *http.Request) {
		var req client.NetworkAttachRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		receivedSwitch = req.SwitchName
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.VM]{
			Status: "success",
			Data:   client.VM{RID: 5, Networks: []client.VMNetwork{{ID: 20}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	// No switch in state — should pick up from env.

	step := &StepFixNIC{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	if receivedSwitch != "env-bridge" {
		t.Errorf("switch = %q, want %q", receivedSwitch, "env-bridge")
	}
}

func TestStepFixNIC_SwitchFromListSwitches(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 50 * time.Millisecond
	fixNICStopMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	t.Setenv("SYLVE_SWITCH", "")
	var pollCount atomic.Int32

	mux := http.NewServeMux()
	// ListSwitches returns a standard switch.
	mux.HandleFunc("/api/network/switch", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SwitchList]{
			Status: "success",
			Data:   client.SwitchList{Standard: []client.StandardSwitch{{Name: "api-bridge"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		st := client.DomainStateRunning
		if count > 1 {
			st = client.DomainStateShutoff
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: st},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/detach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/attach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.VM]{
			Status: "success",
			Data:   client.VM{RID: 5, Networks: []client.VMNetwork{{ID: 20}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	// No switch in state, no env.

	step := &StepFixNIC{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue; error=%v", state.Get("error"))
	}
}

func TestStepFixNIC_ListSwitchesError_Halt(t *testing.T) {
	origPoll := fixNICPollInterval
	fixNICPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { fixNICPollInterval = origPoll })

	t.Setenv("SYLVE_SWITCH", "")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/network/switch", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))

	step := &StepFixNIC{Config: cfg}
	// Should halt: ListSwitches error + no switch available.
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when ListSwitches errors and no switch")
	}
}

func TestStepFixNIC_BootstrapTimeout_ContinuesAnyway(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 20 * time.Millisecond
	fixNICStopMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	var pollCount atomic.Int32
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		// Always NoState during bootstrap, then Shutoff after stop.
		st := client.DomainStateNoState
		if count > 5 {
			st = client.DomainStateShutoff
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: st},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/detach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/attach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.VM]{
			Status: "success",
			Data:   client.VM{RID: 5, Networks: []client.VMNetwork{{ID: 20}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")

	step := &StepFixNIC{Config: cfg}
	// Bootstrap times out (domain stays NoState), but continues to stop+fix.
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue; error=%v", state.Get("error"))
	}
}

func TestStepFixNIC_CtxCancelDuringBootstrap_Halt(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 2 * time.Second
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		// Always NoState, so bootstrap loops until ctx cancel.
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: client.DomainStateNoState},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	step := &StepFixNIC{Config: cfg}
	if step.Run(ctx, state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when ctx cancelled during bootstrap")
	}
}

func TestStepFixNIC_CtxCancelDuringStopPoll_Halt(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 50 * time.Millisecond
	fixNICStopMaxWait = 2 * time.Second
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	var pollCount atomic.Int32
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		if count == 1 {
			// Bootstrap: first poll returns Running → domain created.
			resp := client.APIResponse[client.SimpleVM]{
				Status: "success",
				Data:   client.SimpleVM{RID: 5, State: client.DomainStateRunning},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		// After stop: always Running so stop poll loops until ctx cancel.
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: client.DomainStateRunning},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	step := &StepFixNIC{Config: cfg}
	if step.Run(ctx, state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when ctx cancelled during stop poll")
	}
}

func TestStepFixNIC_PollErrors_Continue(t *testing.T) {
	// Tests that GetSimpleVMByRID errors during bootstrap and stop polls
	// are logged but polling continues.
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 100 * time.Millisecond
	fixNICStopMaxWait = 100 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	var pollCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		// First 2 polls: non-retriable error. Then Running (bootstrap done). Then error. Then Shutoff.
		switch {
		case count <= 2:
			http.Error(w, "bad request", http.StatusBadRequest)
		case count == 3:
			resp := client.APIResponse[client.SimpleVM]{
				Status: "success",
				Data:   client.SimpleVM{RID: 5, State: client.DomainStateRunning},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case count == 4:
			http.Error(w, "bad request", http.StatusBadRequest)
		default:
			resp := client.APIResponse[client.SimpleVM]{
				Status: "success",
				Data:   client.SimpleVM{RID: 5, State: client.DomainStateShutoff},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}
	})
	mux.HandleFunc("/api/vm/network/detach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/attach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, _ *http.Request) {
		// GetVMByRID error after reattach — tests the else branch.
		http.Error(w, "gone", http.StatusGone)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")

	step := &StepFixNIC{Config: cfg}
	// Should succeed despite transient poll errors and GetVMByRID failure at end.
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue; error=%v", state.Get("error"))
	}
}

func TestStepFixNIC_StopVMError_ContinuesAnyway(t *testing.T) {
	origPoll := fixNICPollInterval
	origBootMax := fixNICBootstrapMaxWait
	origStopMax := fixNICStopMaxWait
	fixNICPollInterval = 5 * time.Millisecond
	fixNICBootstrapMaxWait = 50 * time.Millisecond
	fixNICStopMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		fixNICPollInterval = origPoll
		fixNICBootstrapMaxWait = origBootMax
		fixNICStopMaxWait = origStopMax
	})

	var pollCount atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// StopVM returns error (VM might already be stopped).
	mux.HandleFunc("/api/vm/stop/5", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "already stopped", http.StatusConflict)
	})
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		count := pollCount.Add(1)
		st := client.DomainStateRunning
		if count > 1 {
			st = client.DomainStateShutoff
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: 5, State: st},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/detach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/network/attach", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[interface{}]{Status: "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/5", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.VM]{
			Status: "success",
			Data:   client.VM{RID: 5, Networks: []client.VMNetwork{{ID: 20}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_network_id", uint(10))
	state.Put("vm_network_switch", "bridge0")

	step := &StepFixNIC{Config: cfg}
	// StopVM error is only logged, not fatal.
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue; error=%v", state.Get("error"))
	}
}
