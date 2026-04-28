// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packer "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func restoreStartVMDurations(t *testing.T) {
	t.Helper()
	origPoll, origMax, origTaskPoll, origTaskMax, origRetry, origRetryMax := startVMPollInterval, startVMMaxWait, startVMTaskPoll, startVMTaskMaxWait, startVMStartRetry, startVMStartRetryMaxWait
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
		startVMTaskPoll = origTaskPoll
		startVMTaskMaxWait = origTaskMax
		startVMStartRetry = origRetry
		startVMStartRetryMaxWait = origRetryMax
	})
	startVMPollInterval = 1 * time.Millisecond
	startVMMaxWait = 5 * time.Second
	startVMTaskPoll = 1 * time.Millisecond
	startVMTaskMaxWait = 100 * time.Millisecond
	startVMStartRetry = 1 * time.Millisecond
	startVMStartRetryMaxWait = 100 * time.Millisecond
}

func restoreDiscoverIPDurations(t *testing.T) {
	t.Helper()
	origPoll, origMax := discoverIPPollInterval, discoverIPTotalTimeout
	t.Cleanup(func() {
		discoverIPPollInterval = origPoll
		discoverIPTotalTimeout = origMax
	})
	discoverIPPollInterval = 1 * time.Millisecond
	discoverIPTotalTimeout = 5 * time.Second
}

func TestStepDeleteVM_DestroyFalse(t *testing.T) {
	step := &StepDeleteVM{Config: &Config{Destroy: false}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(5))
	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
}

func TestStepDeleteVM_DestroyTrue_NoRID(t *testing.T) {
	step := &StepDeleteVM{Config: &Config{Destroy: true, SylveURL: "http://127.0.0.1:9", SylveToken: "t", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
}

func TestStepDeleteVM_DestroyTrue_Deletes(t *testing.T) {
	var del int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/vm/") {
			atomic.AddInt32(&del, 1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDeleteVM{Config: &Config{Destroy: true, SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(42))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if del != 1 {
		t.Fatalf("DeleteVM not called")
	}
	if state.Get("vm_rid").(uint) != 0 {
		t.Fatalf("vm_rid should be cleared")
	}
}

func TestStepStartVM_StartFails(t *testing.T) {
	restoreStartVMDurations(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/vm/start/3" && r.Method == http.MethodPost {
			http.Error(w, "no", http.StatusConflict)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(3))

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("got %v", got)
	}
}

func TestStepStartVM_RunUntilRunning(t *testing.T) {
	restoreStartVMDurations(t)
	var startCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			atomic.AddInt32(&startCalls, 1)
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if startCalls != 1 {
		t.Fatalf("StartVM calls = %d", startCalls)
	}
}

func TestStepStartVM_PollErrorThenRunning(t *testing.T) {
	restoreStartVMDurations(t)
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getN, 1)
			if n == 1 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if getN < 2 {
		t.Fatalf("expected retry after poll error, getN=%d", getN)
	}
}

func TestStepStartVM_BhyveLogsFetchedWhenNonEmpty(t *testing.T) {
	restoreStartVMDurations(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: "bhyve: guest boot\n"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
}

func TestStepStartVM_ReturnsRunningFromSimpleEndpoint(t *testing.T) {
	restoreStartVMDurations(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
}

func TestStepStartVM_TimesOutWaitingForRunning(t *testing.T) {
	origPoll, origMax := startVMPollInterval, startVMMaxWait
	startVMPollInterval = 1 * time.Millisecond
	startVMMaxWait = 0
	t.Cleanup(func() {
		startVMPollInterval = origPoll
		startVMMaxWait = origMax
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateNoState}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("got %v, want ActionHalt", got)
	}
}

func TestStepStartVM_ContextCancelledDuringPoll(t *testing.T) {
	restoreStartVMDurations(t)
	const rid = uint(11)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == fmt.Sprintf("/api/vm/start/%d", rid) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == fmt.Sprintf("/api/vm/simple/%d", rid) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: rid, State: client.DomainStateShutoff}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", rid)

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("got %v, want ActionHalt", got)
	}
}

// TestStepStartVM_ReleasesVNCListenerBeforeStart verifies that the listener
// pre-bound by selectVNCPort is closed before StartVM is called. Without this
// release bhyve fails immediately with "Address already in use" because both
// the plugin and bhyve try to bind 127.0.0.1:VNCPort.
func TestStepStartVM_ReleasesVNCListenerBeforeStart(t *testing.T) {
	restoreStartVMDurations(t)

	// Pre-bind a listener simulating the one selectVNCPort would hold.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	var startCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			// Verify the listener is already closed before StartVM reaches the server.
			// Attempt to bind the same port: if the plugin still holds it, this will fail.
			probe, probeErr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if probeErr != nil {
				// Listener not yet released — this is the bug we are fixing.
				http.Error(w, "listener still held", http.StatusInternalServerError)
				return
			}
			_ = probe.Close()
			startCalled = true
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v, want ActionContinue", got)
	}
	if !startCalled {
		t.Fatal("StartVM was not called")
	}
	if _, ok := state.GetOk("vnc_view_listener"); ok {
		t.Fatal("vnc_view_listener should have been removed from state")
	}
}

func TestStepStartVM_WaitsForLifecycleTaskThenStarts(t *testing.T) {
	restoreStartVMDurations(t)
	var lifecycleCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/tasks/lifecycle/active/vm/42" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&lifecycleCalls, 1)
			if n < 3 {
				_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{
					Status: "ok",
					Data:   map[string]interface{}{"task": "zvol"},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))
	state.Put("vm_id", uint(42))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if lifecycleCalls < 3 {
		t.Fatalf("expected lifecycle polls before clear, got %d", lifecycleCalls)
	}
}

func TestStepStartVM_LifecycleTaskPollErrorThenClears(t *testing.T) {
	restoreStartVMDurations(t)
	var lifecycleCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/tasks/lifecycle/active/vm/42" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&lifecycleCalls, 1)
			if n == 1 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))
	state.Put("vm_id", uint(42))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if lifecycleCalls < 2 {
		t.Fatalf("expected retry after lifecycle poll error, got %d", lifecycleCalls)
	}
}

func TestStepStartVM_LifecycleTaskDeadlineProceedsAnyway(t *testing.T) {
	restoreStartVMDurations(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/tasks/lifecycle/active/vm/42" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{
				Status: "ok",
				Data:   map[string]interface{}{"still": "running"},
			})
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))
	state.Put("vm_id", uint(42))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
}

func TestStepStartVM_StartVMRetriesUntilSuccess(t *testing.T) {
	restoreStartVMDurations(t)
	var startCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			n := atomic.AddInt32(&startCalls, 1)
			if n < 4 {
				http.Error(w, "lifecycle_task_in_progress", http.StatusConflict)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if startCalls != 4 {
		t.Fatalf("StartVM calls = %d, want 4", startCalls)
	}
}

func TestStepStartVM_ContextCancelledDuringLifecycleWait(t *testing.T) {
	restoreStartVMDurations(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/tasks/lifecycle/active/vm/42" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{
				Status: "ok",
				Data:   map[string]interface{}{"x": 1},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(8 * time.Millisecond)
		cancel()
	}()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))
	state.Put("vm_id", uint(42))

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("got %v, want ActionHalt", got)
	}
}

func TestStepStartVM_ContextCancelledDuringStartRetry(t *testing.T) {
	restoreStartVMDurations(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost {
			http.Error(w, "busy", http.StatusConflict)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(8 * time.Millisecond)
		cancel()
	}()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("got %v, want ActionHalt", got)
	}
}

func TestStepStartVM_BlockedStateCountsAsRunning(t *testing.T) {
	restoreStartVMDurations(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateBlocked}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
}

func TestStepStartVM_IgnoresVNCListenerWrongTypeInState(t *testing.T) {
	restoreStartVMDurations(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/start/9" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/simple/9" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{RID: 9, State: client.DomainStateRunning}})
		case r.URL.Path == "/api/vm/logs/9" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[struct {
				Logs string `json:"logs"`
			}]{Status: "ok", Data: struct {
				Logs string `json:"logs"`
			}{Logs: ""}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepStartVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(9))
	state.Put("vnc_view_listener", "not-a-net-listener")

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
}

func TestStepDeleteVM_DestroyTrue_DeleteErrorStillContinues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/vm/") {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDeleteVM{Config: &Config{Destroy: true, SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(42))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if state.Get("vm_rid").(uint) != 42 {
		t.Fatalf("vm_rid should stay set on delete error")
	}
}

func TestStepShutdown_NoCommunicator(t *testing.T) {
	step := &StepShutdown{Config: &Config{ShutdownCommand: "/sbin/poweroff"}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(1))
	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("got %v", got)
	}
}

func TestStepShutdown_ContextCancelledInWaitLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	step := &StepShutdown{Config: &Config{SylveURL: "http://127.0.0.1:9", SylveToken: "t", TLSSkipVerify: true, ShutdownCommand: "/sbin/poweroff"}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(1))
	state.Put("communicator", new(packer.MockCommunicator))

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("got %v", got)
	}
}

type failStartCommunicator struct {
	packer.MockCommunicator
}

func (f *failStartCommunicator) Start(ctx context.Context, rc *packer.RemoteCmd) error {
	return fmt.Errorf("start failed (expected in test)")
}

func TestStepShutdown_StartFails(t *testing.T) {
	step := &StepShutdown{Config: &Config{ShutdownCommand: "/sbin/poweroff"}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(1))
	state.Put("communicator", &failStartCommunicator{})

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("got %v", got)
	}
}

func TestStepDiscoverIP_NoMAC(t *testing.T) {
	step := &StepDiscoverIP{Config: &Config{}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("got %v", got)
	}
}

func TestStepDiscoverIP_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	step := &StepDiscoverIP{Config: &Config{SylveURL: "http://127.0.0.1:9", SylveToken: "t", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", "aa:bb:cc:dd:ee:ff")

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("got %v", got)
	}
}

func TestStepVNCBootCommand_InvalidBootWait(t *testing.T) {
	step := &StepVNCBootCommand{Config: &Config{BootWait: "not-a-duration"}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("got %v", got)
	}
}

func TestStepVNCBootCommand_InterpolateError(t *testing.T) {
	step := &StepVNCBootCommand{Config: &Config{
		BootWait:    "1ns",
		VMName:      "vm",
		BootCommand: []string{`{{ .Unknown }}`},
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("got %v", got)
	}
}

func TestStepVNCBootCommand_Cleanup(t *testing.T) {
	(&StepVNCBootCommand{}).Cleanup(nil)
}

func TestStepCreateVM_Cleanup_DeletesOnCancel(t *testing.T) {
	var del int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/vm/") {
			atomic.AddInt32(&del, 1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	step := &StepCreateVM{Config: cfg, vmRID: 55, ctx: ctx}

	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(55))

	step.Cleanup(state)

	if del != 1 {
		t.Fatalf("expected DeleteVM, del=%d", del)
	}
}

func TestStepCreateVM_Cleanup_KeepOnError(t *testing.T) {
	var del int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			atomic.AddInt32(&del, 1)
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepCreateVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true, KeepOnError: true}, vmRID: 55}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(55))
	state.Put(multistep.StateHalted, true)

	step.Cleanup(state)
	if del != 0 {
		t.Fatalf("should not delete when keep_on_error and halted")
	}
}

// TestStepCreateVM_Cleanup_ContextCancelledDeletesDespiteKeepOnError ensures a
// cancelled build context still triggers VM deletion when keep_on_error is set.
func TestStepCreateVM_Cleanup_ContextCancelledDeletesDespiteKeepOnError(t *testing.T) {
	var del int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/vm/") {
			atomic.AddInt32(&del, 1)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	step := &StepCreateVM{
		Config: &Config{
			SylveURL:      srv.URL,
			SylveToken:    "tok",
			TLSSkipVerify: true,
			KeepOnError:   true,
		},
		vmRID: 77,
		ctx:   ctx,
	}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(77))
	state.Put(multistep.StateHalted, true)

	step.Cleanup(state)
	if del != 1 {
		t.Fatalf("expected DeleteVM when ctx cancelled, del=%d", del)
	}
}

func TestStepDownloadISO_Run_PollFailed(t *testing.T) {
	restoreDownloadISODurations(t)
	const isoURL = "https://example.com/fail-poll.iso"
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			var data []client.Download
			if n == 1 {
				data = nil
			} else {
				data = []client.Download{{URL: isoURL, Status: client.DownloadStatusFailed, Error: "net down"}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: data})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepDownloadISO{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true, ISODownloadURL: isoURL,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("got %v", got)
	}
}

func TestStepDownloadISO_ExistingInProgressThenDone(t *testing.T) {
	restoreDownloadISODurations(t)
	const isoURL = "https://example.com/inprogress.iso"
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			var dl *client.Download
			if n == 1 {
				dl = &client.Download{URL: isoURL, Status: client.DownloadStatusProcessing, UUID: "u1", Progress: 10}
			} else {
				dl = &client.Download{URL: isoURL, Status: client.DownloadStatusDone, UUID: "u1"}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: []client.Download{*dl}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepDownloadISO{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true, ISODownloadURL: isoURL,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if state.Get("iso_uuid") != "u1" {
		t.Fatalf("iso_uuid = %v", state.Get("iso_uuid"))
	}
}

// Exercise isoProgressTracker.advance when remote HEAD reports a size.
func TestStepDownloadISO_TrackerWithRemoteSize(t *testing.T) {
	restoreDownloadISODurations(t)
	headSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer headSrv.Close()

	u := headSrv.URL + "/track.iso"

	var getN int32
	sylveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			var data []client.Download
			switch n {
			case 1:
				data = nil
			case 2:
				data = []client.Download{{URL: u, Status: client.DownloadStatusProcessing, UUID: "tid", Progress: 50}}
			default:
				data = []client.Download{{URL: u, Status: client.DownloadStatusDone, UUID: "tid"}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: data})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sylveSrv.Close()

	step := &StepDownloadISO{Config: &Config{
		SylveURL: sylveSrv.URL, SylveToken: "tok", TLSSkipVerify: true, ISODownloadURL: u,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if got := step.Run(ctx, state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
}

func TestStepShutdown_VMStops(t *testing.T) {
	restoreShutdownDurations(t)
	const rid = uint(2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/api/vm/stop/2" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case p == "/api/vm/simple/2" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			sm := client.SimpleVM{RID: rid, State: client.DomainStateShutoff}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: sm})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepShutdown{Config: &Config{
		SylveURL:        srv.URL,
		SylveToken:      "tok",
		TLSSkipVerify:   true,
		ShutdownCommand: "/bin/true",
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", rid)
	state.Put("communicator", new(packer.MockCommunicator))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
}

func TestStepDiscoverIP_DHCPLease(t *testing.T) {
	restoreDiscoverIPDurations(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/network/dhcp/lease" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
			Status: "ok",
			Data: client.Leases{File: []client.FileLease{{
				MAC: "aa:bb:cc:dd:ee:ff",
				IP:  "10.11.12.13",
			}}},
		})
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", "aa:bb:cc:dd:ee:ff")

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if state.Get("instance_ip") != "10.11.12.13" {
		t.Fatalf("instance_ip = %v", state.Get("instance_ip"))
	}
}
