// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func TestStepShutdown_CleanupNoOp(t *testing.T) {
	step := &StepShutdown{Config: &Config{}}
	state := newTestState(t)
	// Cleanup is a no-op but must be called for coverage.
	step.Cleanup(state)
}

func TestStepShutdown_ShutdownCommand_NoCommunicator_Halt(t *testing.T) {
	// If ShutdownCommand is set but communicator is not in state, step should halt.
	cfg := &Config{ShutdownCommand: "shutdown -h now"}
	state := newTestState(t)
	state.Put("vm_rid", uint(3))
	// communicator intentionally not put in state.

	step := &StepShutdown{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt when communicator missing, got %v", action)
	}
}

func TestStepShutdown_NoShutdownCommand_StopsAndWaits(t *testing.T) {
	origPoll := shutdownPollInterval
	origMax := shutdownMaxWait
	shutdownPollInterval = 10 * time.Millisecond
	shutdownMaxWait = 2 * time.Second
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})

	// POST /api/vm/stop/:rid — success
	// GET  /api/vm/simple/:rid?type=rid — returns state=0 (stopped)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/stop/9", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/9", func(w http.ResponseWriter, _ *http.Request) {
		type simpleVM struct {
			RID   uint `json:"rid"`
			State int  `json:"state"`
		}
		// State=0 means stopped.
		resp := map[string]interface{}{"status": "success", "data": simpleVM{RID: 9, State: 0}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:        srv.URL,
		TLSSkipVerify:   true,
		ShutdownCommand: "", // no shutdown command — go straight to StopVM
	}
	state := newTestState(t)
	state.Put("vm_rid", uint(9))

	step := &StepShutdown{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
}

func TestStepShutdown_VMNotFound_Continue(t *testing.T) {
	origPoll := shutdownPollInterval
	origMax := shutdownMaxWait
	shutdownPollInterval = 10 * time.Millisecond
	shutdownMaxWait = 2 * time.Second
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/stop/13", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// Return 404 so IsNotFound triggers the "no longer exists" branch.
	mux.HandleFunc("/api/vm/simple/13", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"status":"error","message":"not found"}`, http.StatusNotFound)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(13))

	step := &StepShutdown{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue when VM not found, got %v", action)
	}
}

func TestStepShutdown_ContextCancel_Halt(t *testing.T) {
	origPoll := shutdownPollInterval
	origMax := shutdownMaxWait
	shutdownPollInterval = 5 * time.Millisecond
	shutdownMaxWait = 30 * time.Second
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/stop/15", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// Always return Running so we stay in the loop until ctx is cancelled.
	mux.HandleFunc("/api/vm/simple/15", func(w http.ResponseWriter, _ *http.Request) {
		type simpleVM struct {
			RID   uint `json:"rid"`
			State int  `json:"state"`
		}
		resp := map[string]interface{}{"status": "success", "data": simpleVM{RID: 15, State: 1}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(15))

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a short delay so the step has time to enter the poll loop.
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	step := &StepShutdown{Config: cfg}
	action := step.Run(ctx, state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt on context cancel, got %v", action)
	}
}

func TestStepShutdown_Timeout_ForcedStop(t *testing.T) {
	origPoll := shutdownPollInterval
	origMax := shutdownMaxWait
	shutdownPollInterval = 5 * time.Millisecond
	shutdownMaxWait = 20 * time.Millisecond
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/stop/17", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// Always Running so the timeout path is hit.
	mux.HandleFunc("/api/vm/simple/17", func(w http.ResponseWriter, _ *http.Request) {
		type simpleVM struct {
			RID   uint `json:"rid"`
			State int  `json:"state"`
		}
		resp := map[string]interface{}{"status": "success", "data": simpleVM{RID: 17, State: 1}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(17))

	step := &StepShutdown{Config: cfg}
	action := step.Run(context.Background(), state)
	// Forced stop returns ActionContinue.
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue after forced stop, got %v", action)
	}
}

func TestStepShutdown_ShutdownCommand_MockCommunicator_Success(t *testing.T) {
	origPoll := shutdownPollInterval
	origMax := shutdownMaxWait
	shutdownPollInterval = 10 * time.Millisecond
	shutdownMaxWait = 5 * time.Second
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})

	const rid uint = 71
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/vm/stop/%d", rid), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc(fmt.Sprintf("/api/vm/simple/%d", rid), func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateShutoff},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:        srv.URL,
		TLSSkipVerify:   true,
		ShutdownCommand: "/sbin/shutdown now",
	}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("communicator", new(packersdk.MockCommunicator))

	step := &StepShutdown{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue with shutdown command flow, got %v; error=%v", action, state.Get("error"))
	}
}

func TestStepShutdown_PostStopVMAfterShutdownCommand_NonFatal(t *testing.T) {
	origPoll := shutdownPollInterval
	origMax := shutdownMaxWait
	shutdownPollInterval = 10 * time.Millisecond
	shutdownMaxWait = 5 * time.Second
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})

	const rid uint = 73
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/vm/stop/%d", rid), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, "already stopped", http.StatusInternalServerError)
	})
	mux.HandleFunc(fmt.Sprintf("/api/vm/simple/%d", rid), func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateShutoff},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:        srv.URL,
		TLSSkipVerify:   true,
		ShutdownCommand: "/bin/true",
	}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("communicator", new(packersdk.MockCommunicator))

	step := &StepShutdown{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("expected Continue when Stop VM errors non-fatally")
	}
}

type shutdownFailStartCommunicator struct {
	packersdk.MockCommunicator
}

func (f *shutdownFailStartCommunicator) Start(_ context.Context, _ *packersdk.RemoteCmd) error {
	return fmt.Errorf("shutdown start failed")
}

func TestStepShutdown_ShutdownCommand_CommunicatorStartError(t *testing.T) {
	cfg := &Config{SylveURL: "http://ignored", ShutdownCommand: "halt"}
	state := newTestState(t)
	state.Put("vm_rid", uint(3))
	state.Put("communicator", &shutdownFailStartCommunicator{})

	step := &StepShutdown{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected ActionHalt when communicator Start fails")
	}
}

func TestStepShutdown_GetSimpleVM_TransientHTTPErrorRetries(t *testing.T) {
	origPoll := shutdownPollInterval
	origMax := shutdownMaxWait
	shutdownPollInterval = 10 * time.Millisecond
	shutdownMaxWait = 5 * time.Second
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})

	const rid uint = 75
	var getN int32
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/vm/stop/%d", rid), func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc(fmt.Sprintf("/api/vm/simple/%d", rid), func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&getN, 1)
		if n == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateShutoff},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)

	if (&StepShutdown{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("expected Continue after transient poll error")
	}
	if atomic.LoadInt32(&getN) < 2 {
		t.Fatal("expected GetSimpleVM retry after transient error")
	}
}

func TestStepShutdown_Timeout_SecondForceStop_ErrorsLogged(t *testing.T) {
	origPoll := shutdownPollInterval
	origMax := shutdownMaxWait
	shutdownPollInterval = 5 * time.Millisecond
	shutdownMaxWait = 25 * time.Millisecond
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})

	const rid uint = 77
	var stopCalls int32
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/api/vm/stop/%d", rid), func(w http.ResponseWriter, _ *http.Request) {
		c := atomic.AddInt32(&stopCalls, 1)
		if c >= 2 {
			http.Error(w, "cannot stop again", http.StatusInternalServerError)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc(fmt.Sprintf("/api/vm/simple/%d", rid), func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"rid":   rid,
				"state": client.DomainStateRunning,
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

	if (&StepShutdown{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatal("expected Continue after deadline force-stop attempt")
	}
	if atomic.LoadInt32(&stopCalls) < 2 {
		t.Fatalf("expected forced second Stop VM call")
	}
}
