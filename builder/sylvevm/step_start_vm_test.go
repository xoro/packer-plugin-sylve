// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylvevm

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

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func TestStepStartVM_CleanupNoOp(t *testing.T) {
	step := &StepStartVM{Config: &Config{}}
	step.Cleanup(newTestState(t))
}

func TestStepStartVM_Success(t *testing.T) {
	// Reduce poll intervals to make the test fast.
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTask := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = 2 * time.Second
	startVMTaskPoll = 10 * time.Millisecond
	startVMTaskMaxWait = 100 * time.Millisecond
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 100 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTask
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	// /api/vm/:rid/tasks — no active lifecycle tasks
	// /api/vm/start/:rid — success
	// /api/vm/simple/:rid — running (state=1)
	// /api/vm/:rid/logs — empty (optional)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/5", func(w http.ResponseWriter, _ *http.Request) {
		// Null data means no active lifecycle task.
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/5", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/5", func(w http.ResponseWriter, _ *http.Request) {
		type simpleVM struct {
			RID   uint `json:"rid"`
			State int  `json:"state"`
		}
		resp := map[string]interface{}{"status": "success", "data": simpleVM{RID: 5, State: 1}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/5", func(w http.ResponseWriter, _ *http.Request) {
		type logsData struct {
			Logs string `json:"logs"`
		}
		resp := map[string]interface{}{"status": "success", "data": logsData{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{
		SylveURL:      srv.URL,
		TLSSkipVerify: true,
	}
	state := newTestState(t)
	state.Put("vm_rid", uint(5))
	state.Put("vm_id", uint(5))

	step := &StepStartVM{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
}

func TestStepStartVM_StartError_Halt(t *testing.T) {
	origPoll := startVMPollInterval
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	origTaskPoll := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 30 * time.Millisecond
	startVMTaskPoll = 10 * time.Millisecond
	startVMTaskMaxWait = 30 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/7", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/7", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"status":"error","message":"forbidden"}`, http.StatusForbidden)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(7))
	state.Put("vm_id", uint(7))

	step := &StepStartVM{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt, got %v", action)
	}
}

func TestStepStartVM_ContextCancel_Halt(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTaskPoll := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 5 * time.Millisecond
	startVMMaxWait = 30 * time.Second
	startVMTaskPoll = 5 * time.Millisecond
	startVMTaskMaxWait = 30 * time.Millisecond
	startVMStartRetry = 5 * time.Millisecond
	startVMStartRetryMaxWait = 100 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/19", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/19", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// Keep returning Running so the VM never reaches Running before ctx cancel.
	// Actually return Running immediately to get past the start-retry loop,
	// then the poll loop will spin until ctx is cancelled.
	mux.HandleFunc("/api/vm/simple/19", func(w http.ResponseWriter, _ *http.Request) {
		// Return state=0 (not running) so the poll loop continues until cancel.
		type simpleVM struct {
			RID   uint `json:"rid"`
			State int  `json:"state"`
		}
		resp := map[string]interface{}{"status": "success", "data": simpleVM{RID: 19, State: 0}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(19))
	state.Put("vm_id", uint(19))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()

	step := &StepStartVM{Config: cfg}
	action := step.Run(ctx, state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt on context cancel, got %v", action)
	}
}

func TestStepStartVM_Timeout_Halt(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTaskPoll := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 5 * time.Millisecond
	startVMMaxWait = 20 * time.Millisecond
	startVMTaskPoll = 5 * time.Millisecond
	startVMTaskMaxWait = 30 * time.Millisecond
	startVMStartRetry = 5 * time.Millisecond
	startVMStartRetryMaxWait = 100 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/21", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/21", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	// Always return state=0 so the poll loop times out.
	mux.HandleFunc("/api/vm/simple/21", func(w http.ResponseWriter, _ *http.Request) {
		type simpleVM struct {
			RID   uint `json:"rid"`
			State int  `json:"state"`
		}
		resp := map[string]interface{}{"status": "success", "data": simpleVM{RID: 21, State: 0}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(21))
	state.Put("vm_id", uint(21))

	step := &StepStartVM{Config: cfg}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt on timeout, got %v", action)
	}
}

func TestStepStartVM_BlockedStateCountsAsRunning(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTask := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = 2 * time.Second
	startVMTaskPoll = 10 * time.Millisecond
	startVMTaskMaxWait = 50 * time.Millisecond
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTask
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(52)
	const vmid = uint(52)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/52", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/52", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/52", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data: client.SimpleVM{
				RID:   rid,
				State: client.DomainStateBlocked,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/52", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data":   map[string]string{"logs": "last line"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", vmid)

	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("Blocked should count as running; error=%v", state.Get("error"))
	}
}

func TestStepStartVM_LifecycleTaskBusy_UntilDeadlineThenStarts(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTask := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = 2 * time.Second
	startVMTaskPoll = 5 * time.Millisecond
	startVMTaskMaxWait = 25 * time.Millisecond
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTask
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(53)
	const vmid = uint(53)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/53", func(w http.ResponseWriter, _ *http.Request) {
		// Non-nil data means an active lifecycle task is present.
		resp := map[string]interface{}{
			"status": "success",
			"data":   map[string]interface{}{"kind": "fake"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/53", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/53", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateRunning},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/53", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": map[string]string{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", vmid)

	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("expected start after lifecycle deadline; error=%v", state.Get("error"))
	}
}

func TestStepStartVM_StartRetriesOnceThenSucceeds(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTask := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = 2 * time.Second
	startVMTaskPoll = 10 * time.Millisecond
	startVMTaskMaxWait = 50 * time.Millisecond
	startVMStartRetry = 5 * time.Millisecond
	startVMStartRetryMaxWait = 100 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTask
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(54)
	var startCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/54", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/54", func(w http.ResponseWriter, r *http.Request) {
		startCalls++
		if startCalls == 1 {
			http.Error(w, `{"status":"error"}`, http.StatusConflict)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/54", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateRunning},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/54", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": map[string]string{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", rid)

	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("expected retry then success")
	}
	if startCalls < 2 {
		t.Fatalf("expected at least two Start attempts, got %d", startCalls)
	}
}

func TestStepStartVM_LifecycleTaskPollHTTPErrorStillProceeds(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTaskPoll := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 15 * time.Millisecond
	startVMMaxWait = time.Second
	startVMTaskPoll = 5 * time.Millisecond
	startVMTaskMaxWait = 45 * time.Millisecond
	startVMStartRetry = 15 * time.Millisecond
	startVMStartRetryMaxWait = 60 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(62)
	var lifeCalls int
	writeNullLifecycle := func(w http.ResponseWriter) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/62", func(w http.ResponseWriter, _ *http.Request) {
		lifeCalls++
		if lifeCalls <= 2 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		writeNullLifecycle(w)
	})
	mux.HandleFunc("/api/vm/start/62", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		writeNullLifecycle(w)
	})
	mux.HandleFunc("/api/vm/simple/62", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{Status: "success", Data: client.SimpleVM{RID: rid, State: client.DomainStateRunning}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/62", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": map[string]string{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", rid)

	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
	if lifeCalls < 3 {
		t.Fatalf("expected repeated lifecycle polls, calls=%d", lifeCalls)
	}
}

func TestStepStartVM_Cleanup_StopsRunningGuest(t *testing.T) {
	origPoll := startVMPollInterval
	startVMPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { startVMPollInterval = origPoll })

	const rid = uint(90)
	var simpleN int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple/90", func(w http.ResponseWriter, _ *http.Request) {
		simpleN++
		st := int(client.DomainStateShutoff)
		if simpleN == 1 {
			st = int(client.DomainStateRunning)
		}
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"rid": rid, "state": st,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/90", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	step := &StepStartVM{Config: cfg}

	step.Cleanup(state)
	if simpleN < 2 {
		t.Fatalf("cleanup should poll until stopped")
	}
}

func TestStepStartVM_Cleanup_GetVMFails_NoStop(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple/91", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "db down", http.StatusInternalServerError)
	})
	var stopCalls int
	mux.HandleFunc("/api/vm/stop/91", func(http.ResponseWriter, *http.Request) {
		stopCalls++
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(91))
	(&StepStartVM{Config: cfg}).Cleanup(state)
	if stopCalls != 0 {
		t.Fatalf("StopVM should not be called when lookup fails")
	}
}

func TestStepStartVM_Cleanup_GuestAlreadyShutoff(t *testing.T) {
	var stopCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple/92", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data":   map[string]interface{}{"rid": uint(92), "state": int(client.DomainStateShutoff)},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/92", func(http.ResponseWriter, *http.Request) { stopCalls++ })
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(92))
	(&StepStartVM{Config: cfg}).Cleanup(state)
	if stopCalls != 0 {
		t.Fatal("stopped guest that were already halted")
	}
}

func TestStepStartVM_Cleanup_StopVMAPIErrorThenReturns(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple/93", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"rid": uint(93), "state": int(client.DomainStateRunning),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/93", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, "fail-stop", http.StatusConflict)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", uint(93))
	(&StepStartVM{Config: cfg}).Cleanup(state)
}

func TestStepStartVM_NoVMIDSkipsLifecyclePoll(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTaskPoll := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = time.Second
	startVMTaskPoll = 50 * time.Millisecond
	startVMTaskMaxWait = 200 * time.Millisecond
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(70)
	var lifeCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/70", func(w http.ResponseWriter, _ *http.Request) {
		lifeCalls++
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/70", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/70", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateRunning},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/70", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": map[string]string{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
	if lifeCalls != 0 {
		t.Fatalf("lifecycle endpoint should never be polled without vm_id, calls=%d", lifeCalls)
	}
}

func TestStepStartVM_SimplePollErrorThenRecover(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTaskPoll := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 15 * time.Millisecond
	startVMMaxWait = time.Second
	startVMTaskPoll = 5 * time.Millisecond
	startVMTaskMaxWait = 20 * time.Millisecond
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 40 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(71)
	var simpleCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/71", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/71", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/71", func(w http.ResponseWriter, _ *http.Request) {
		simpleCalls++
		// Use a non-retriable status so client.do does not succeed on retry
		// within the same GetSimpleVMByRID call (502/503/504 are retried).
		if simpleCalls < 3 {
			http.Error(w, "backend", http.StatusInternalServerError)
			return
		}
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateRunning},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/71", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": map[string]string{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", rid)
	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
	if simpleCalls < 3 {
		t.Fatalf("expected retries on simple lookups, calls=%d", simpleCalls)
	}
}

func TestStepStartVM_BlockedGuestCountsAsRunning(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTask := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = time.Second
	startVMTaskPoll = 10 * time.Millisecond
	startVMTaskMaxWait = 50 * time.Millisecond
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 50 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTask
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(72)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/72", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/72", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/72", func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateBlocked},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/logs/72", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]string{
				"logs": "note: installer still visible",
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
	state.Put("vm_id", rid)
	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
}

func TestStepStartVM_ContextCancelDuringLifecycleWait(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTaskPoll := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	startVMPollInterval = 80 * time.Millisecond
	startVMMaxWait = time.Second
	startVMTaskPoll = time.Millisecond
	startVMTaskMaxWait = time.Second
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
	})

	const rid = uint(73)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/73", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"status": "success",
			"data":   map[string]string{"phase": "init"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", rid)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if (&StepStartVM{Config: cfg}).Run(ctx, state) != multistep.ActionHalt {
		t.Fatal("expected halt on cancelled context during lifecycle polling")
	}
}

func TestStepStartVM_ContextCancelDuringStartRetries(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTaskPoll := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = time.Second
	startVMMaxWait = time.Second
	startVMTaskPoll = time.Millisecond
	startVMTaskMaxWait = time.Millisecond
	startVMStartRetry = 50 * time.Millisecond
	startVMStartRetryMaxWait = time.Second
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	const rid = uint(74)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/74", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/74", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "lifecycle_task_in_progress", http.StatusConflict)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", rid)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	action := (&StepStartVM{Config: cfg}).Run(ctx, state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected halt, got %v err=%v", action, state.Get("error"))
	}
	if ctx.Err() == nil {
		t.Fatal("expected cancelled context error in state bag")
	}
}

func TestStepStartVM_DomainBlocked_CountedAsStarted(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origTaskPoll := startVMTaskPoll
	origTaskMax := startVMTaskMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = 2 * time.Second
	startVMTaskPoll = 5 * time.Millisecond
	startVMTaskMaxWait = 30 * time.Millisecond
	startVMStartRetry = 5 * time.Millisecond
	startVMStartRetryMaxWait = 30 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	rid := uint(81)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tasks/lifecycle/active/vm/81", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/start/81", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/81", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": client.SimpleVM{RID: rid, State: client.DomainStateBlocked}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)
	state.Put("vm_id", uint(400))

	step := &StepStartVM{Config: cfg}
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("blocked state should behave like running: err=%v", state.Get("error"))
	}
}

func TestStepStartVM_NoVmID_SkipsLifecycleWait(t *testing.T) {
	origPoll := startVMPollInterval
	origMax := startVMMaxWait
	origRetry := startVMStartRetry
	origRetryMax := startVMStartRetryMaxWait
	startVMPollInterval = 10 * time.Millisecond
	startVMMaxWait = 2 * time.Second
	startVMStartRetry = 10 * time.Millisecond
	startVMStartRetryMaxWait = 200 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})

	rid := uint(91)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/start/"+fmt.Sprint(rid), func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/simple/"+fmt.Sprint(rid), func(w http.ResponseWriter, _ *http.Request) {
		resp := client.APIResponse[client.SimpleVM]{
			Status: "success",
			Data:   client.SimpleVM{RID: rid, State: client.DomainStateRunning},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)

	if (&StepStartVM{Config: cfg}).Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatal("want continue without vm_id lifecycle gate")
	}
}

func TestStepStartVM_Cleanup_StopsRunningAndPollsShutoff(t *testing.T) {
	origPoll := startVMPollInterval
	origCleanupMax := startVMCleanupMaxWait
	startVMPollInterval = 5 * time.Millisecond
	startVMCleanupMaxWait = 200 * time.Millisecond
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMCleanupMaxWait = origCleanupMax
	})

	rid := uint(101)
	var stopN int32
	var getN int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple/"+fmt.Sprint(rid), func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&getN, 1)
		st := client.DomainStateRunning
		if n >= 3 {
			st = client.DomainStateShutoff
		}
		resp := client.APIResponse[client.SimpleVM]{Status: "success", Data: client.SimpleVM{RID: rid, State: st}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/vm/stop/"+fmt.Sprint(rid), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt32(&stopN, 1)
		resp := map[string]interface{}{"status": "success", "data": nil}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)

	step := &StepStartVM{Config: cfg}
	step.Cleanup(state)
	if atomic.LoadInt32(&stopN) != 1 {
		t.Fatalf("stop calls=%d", stopN)
	}
}

func TestStepStartVM_Cleanup_GetSimpleError_Returns(t *testing.T) {
	rid := uint(103)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/vm/simple/"+fmt.Sprint(rid), func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"status":"error"}`, http.StatusBadGateway)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_rid", rid)

	(&StepStartVM{Config: cfg}).Cleanup(state)
}
