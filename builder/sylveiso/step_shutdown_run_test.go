// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/json"
	"fmt"
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

func restoreShutdownDurations(t *testing.T) {
	t.Helper()
	origPoll, origMax := shutdownPollInterval, shutdownMaxWait
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})
	shutdownPollInterval = 1 * time.Millisecond
	shutdownMaxWait = 5 * time.Second
}

// TestStepShutdown_SuccessWhenVMNotRunning covers the poll loop exit when
// GetSimpleVMByRID reports a non-Running state (e.g. Shutoff).
func TestStepShutdown_SuccessWhenVMNotRunning(t *testing.T) {
	restoreShutdownDurations(t)
	const rid = 7

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == fmt.Sprintf("/api/vm/stop/%d", rid) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/simple/") && r.Method == http.MethodGet && r.URL.Query().Get("type") == "rid":
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{
				Status: "ok",
				Data: client.SimpleVM{
					RID:   rid,
					State: client.DomainStateShutoff,
				},
			})
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
	state.Put("vm_rid", uint(rid))
	state.Put("communicator", new(packer.MockCommunicator))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}

// TestStepShutdown_SimpleVMPollErrorThenShutoff exercises GetSimpleVMByRID errors
// followed by a successful non-Running state.
func TestStepShutdown_SimpleVMPollErrorThenShutoff(t *testing.T) {
	restoreShutdownDurations(t)
	const rid = 7
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == fmt.Sprintf("/api/vm/stop/%d", rid) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/simple/") && r.Method == http.MethodGet && r.URL.Query().Get("type") == "rid":
			n := atomic.AddInt32(&getN, 1)
			if n == 1 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{
				Status: "ok",
				Data: client.SimpleVM{
					RID:   rid,
					State: client.DomainStateShutoff,
				},
			})
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
	state.Put("vm_rid", uint(rid))
	state.Put("communicator", new(packer.MockCommunicator))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if getN < 2 {
		t.Fatalf("expected retry after simple VM poll error, getN=%d", getN)
	}
}

// TestStepShutdown_PostPoweroffStopError covers the non-fatal Sylve StopVM error
// path after the SSH shutdown command completes.
func TestStepShutdown_PostPoweroffStopError(t *testing.T) {
	restoreShutdownDurations(t)
	const rid = 7

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == fmt.Sprintf("/api/vm/stop/%d", rid) && r.Method == http.MethodPost:
			http.Error(w, "stop failed", http.StatusInternalServerError)
			return
		case strings.HasPrefix(r.URL.Path, "/api/vm/simple/") && r.Method == http.MethodGet && r.URL.Query().Get("type") == "rid":
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{
				Status: "ok",
				Data: client.SimpleVM{
					RID:   rid,
					State: client.DomainStateShutoff,
				},
			})
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
	state.Put("vm_rid", uint(rid))
	state.Put("communicator", new(packer.MockCommunicator))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}

// TestStepShutdown_SimpleVMNotFoundTreatsAsStopped covers the 404 path while
// waiting for the VM to report a non-Running state (e.g. concurrent delete).
func TestStepShutdown_SimpleVMNotFoundTreatsAsStopped(t *testing.T) {
	restoreShutdownDurations(t)
	const rid = 7

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == fmt.Sprintf("/api/vm/stop/%d", rid) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/simple/") && r.Method == http.MethodGet && r.URL.Query().Get("type") == "rid":
			http.NotFound(w, r)
			return
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
	state.Put("vm_rid", uint(rid))
	state.Put("communicator", new(packer.MockCommunicator))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}

// TestStepShutdown_WaitTimeoutForcesStop covers the poll deadline branch when
// the VM stays Running until the wait window elapses.
func TestStepShutdown_WaitTimeoutForcesStop(t *testing.T) {
	origPoll, origMax := shutdownPollInterval, shutdownMaxWait
	shutdownPollInterval = 1 * time.Millisecond
	shutdownMaxWait = 0
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})
	const rid = 7

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == fmt.Sprintf("/api/vm/stop/%d", rid) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/simple/") && r.Method == http.MethodGet && r.URL.Query().Get("type") == "rid":
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{
				Status: "ok",
				Data: client.SimpleVM{
					RID:   rid,
					State: client.DomainStateRunning,
				},
			})
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
	state.Put("vm_rid", uint(rid))
	state.Put("communicator", new(packer.MockCommunicator))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}

// TestStepShutdown_WaitTimeoutForceStopError logs when the forced StopVM after
// the shutdown wait deadline fails.
func TestStepShutdown_WaitTimeoutForceStopError(t *testing.T) {
	origPoll, origMax := shutdownPollInterval, shutdownMaxWait
	shutdownPollInterval = 1 * time.Millisecond
	shutdownMaxWait = 0
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})
	const rid = 7

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == fmt.Sprintf("/api/vm/stop/%d", rid) && r.Method == http.MethodPost:
			http.Error(w, "stop failed", http.StatusInternalServerError)
			return
		case strings.HasPrefix(r.URL.Path, "/api/vm/simple/") && r.Method == http.MethodGet && r.URL.Query().Get("type") == "rid":
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{
				Status: "ok",
				Data: client.SimpleVM{
					RID:   rid,
					State: client.DomainStateRunning,
				},
			})
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
	state.Put("vm_rid", uint(rid))
	state.Put("communicator", new(packer.MockCommunicator))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}

// TestStepShutdown_SylveUnreachableAfterShutdown verifies that when the Sylve
// API is unreachable (connection refused), the shutdown wait hits the deadline
// (shutdownMaxWait=0), attempts force-stop, and still continues the build.
func TestStepShutdown_SylveUnreachableAfterShutdown(t *testing.T) {
	origPoll, origMax := shutdownPollInterval, shutdownMaxWait
	shutdownPollInterval = 1 * time.Millisecond
	shutdownMaxWait = 0
	t.Cleanup(func() {
		shutdownPollInterval = origPoll
		shutdownMaxWait = origMax
	})
	const rid = 7

	// Close a server immediately so all requests get connection refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	step := &StepShutdown{Config: &Config{
		SylveURL:        srv.URL,
		SylveToken:      "tok",
		TLSSkipVerify:   true,
		ShutdownCommand: "/bin/true",
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(rid))
	state.Put("communicator", new(packer.MockCommunicator))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
}
