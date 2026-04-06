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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func restoreRestartStepDurations(t *testing.T) {
	t.Helper()
	orig := struct {
		shutoffPoll, shutoffMax, taskPoll, taskMax, startRetry, startMax, runPoll, runMax time.Duration
	}{
		restartAfterInstallShutoffPoll,
		restartAfterInstallShutoffMaxWait,
		restartAfterInstallTaskPoll,
		restartAfterInstallTaskMaxWait,
		restartAfterInstallStartRetry,
		restartAfterInstallStartMaxWait,
		restartAfterInstallRunningPoll,
		restartAfterInstallRunningMaxWait,
	}
	t.Cleanup(func() {
		restartAfterInstallShutoffPoll = orig.shutoffPoll
		restartAfterInstallShutoffMaxWait = orig.shutoffMax
		restartAfterInstallTaskPoll = orig.taskPoll
		restartAfterInstallTaskMaxWait = orig.taskMax
		restartAfterInstallStartRetry = orig.startRetry
		restartAfterInstallStartMaxWait = orig.startMax
		restartAfterInstallRunningPoll = orig.runPoll
		restartAfterInstallRunningMaxWait = orig.runMax
	})
	restartAfterInstallShutoffPoll = 1 * time.Millisecond
	restartAfterInstallShutoffMaxWait = 5 * time.Second
	restartAfterInstallTaskPoll = 1 * time.Millisecond
	restartAfterInstallTaskMaxWait = 5 * time.Second
	restartAfterInstallStartRetry = 1 * time.Millisecond
	restartAfterInstallStartMaxWait = 5 * time.Second
	restartAfterInstallRunningPoll = 1 * time.Millisecond
	restartAfterInstallRunningMaxWait = 5 * time.Second
}

func TestStepRestartAfterInstall_Run_Success(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}
func TestStepRestartAfterInstall_StartVMRetryThenSuccess(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls, startCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			n := atomic.AddInt32(&startCalls, 1)
			if n == 1 {
				http.Error(w, "lifecycle_task_in_progress", http.StatusConflict)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if startCalls < 2 {
		t.Fatalf("expected StartVM retry, startCalls=%d", startCalls)
	}
}

func TestStepRestartAfterInstall_StopVMError(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			http.Error(w, "stop failed", http.StatusInternalServerError)
			return
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}
func TestStepRestartAfterInstall_DisableISOError(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			http.Error(w, "disable failed", http.StatusInternalServerError)
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}

func TestStepRestartAfterInstall_NoVmID_SkipsLifecycleWait(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	// Deliberately omit vm_id — lifecycle task wait loop is skipped.
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}
func TestStepRestartAfterInstall_TaskPollErrorThenSuccess(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	var lifeCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			n := atomic.AddInt32(&lifeCalls, 1)
			if n == 1 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if lifeCalls < 2 {
		t.Fatalf("expected lifecycle poll retry, lifeCalls=%d", lifeCalls)
	}
}

func TestStepRestartAfterInstall_VNCReconnectError(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)
	state.Put("vnc_reconnect", vncReconnectFunc(func(context.Context, packersdk.Ui) error {
		return fmt.Errorf("reconnect failed (expected in test)")
	}))

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}

func TestStepRestartAfterInstall_Run_SuccessDomainNoState(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateNoState
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}
func TestStepRestartAfterInstall_ShutoffGetVMErrorsUntilDeadline(t *testing.T) {
	restoreRestartStepDurations(t)
	restartAfterInstallShutoffPoll = 5 * time.Millisecond
	restartAfterInstallShutoffMaxWait = 0

	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			if n == 1 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{
				ID:    vmID,
				RID:   vmRID,
				State: client.DomainStateRunning,
			}})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if getVMCalls < 2 {
		t.Fatalf("expected shutoff error then run poll, getVMCalls=%d", getVMCalls)
	}
}

// TestStepRestartAfterInstall_ShutoffGetVMTransientErrorBeforeRecover covers the
// shutoff-loop continue branch when GetVMByRID fails before the shutoff deadline.
func TestStepRestartAfterInstall_ShutoffGetVMTransientErrorBeforeRecover(t *testing.T) {
	restoreRestartStepDurations(t)
	restartAfterInstallShutoffPoll = 2 * time.Millisecond
	restartAfterInstallShutoffMaxWait = 30 * time.Second

	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			if n == 1 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateRunning,
				StoppedAt: time.Now(),
			}})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if getVMCalls < 2 {
		t.Fatalf("expected shutoff error then success, getVMCalls=%d", getVMCalls)
	}
}

func TestStepRestartAfterInstall_ShutoffProceedsWithoutStoppedAt(t *testing.T) {
	restoreRestartStepDurations(t)
	restartAfterInstallShutoffPoll = 5 * time.Millisecond
	restartAfterInstallShutoffMaxWait = 0

	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			if n == 1 {
				_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{
					ID:        vmID,
					RID:       vmRID,
					State:     client.DomainStateShutoff,
					StoppedAt: time.Time{},
				}})
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{
				ID:    vmID,
				RID:   vmRID,
				State: client.DomainStateRunning,
			}})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
}

func TestStepRestartAfterInstall_StartVMDeadlineExhausted(t *testing.T) {
	restoreRestartStepDurations(t)
	restartAfterInstallShutoffPoll = 5 * time.Millisecond
	restartAfterInstallShutoffMaxWait = 0
	restartAfterInstallStartRetry = 5 * time.Millisecond
	restartAfterInstallStartMaxWait = 40 * time.Millisecond

	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateRunning,
				StoppedAt: time.Now(),
			}})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			http.Error(w, "start failed", http.StatusInternalServerError)
			return
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}

func TestStepRestartAfterInstall_LifecycleTaskDeadlineProceeds(t *testing.T) {
	restoreRestartStepDurations(t)
	restartAfterInstallShutoffPoll = 5 * time.Millisecond
	restartAfterInstallShutoffMaxWait = 0
	restartAfterInstallTaskPoll = 10 * time.Millisecond
	restartAfterInstallTaskMaxWait = 35 * time.Millisecond
	restartAfterInstallStartRetry = 5 * time.Millisecond
	restartAfterInstallStartMaxWait = 2 * time.Minute

	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var lifeCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateRunning,
				StoppedAt: time.Now(),
			}})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			n := atomic.AddInt32(&lifeCalls, 1)
			if n < 10 {
				_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{
					Status: "ok",
					Data:   map[string]interface{}{"task": "stop"},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if lifeCalls < 2 {
		t.Fatalf("expected multiple lifecycle polls, lifeCalls=%d", lifeCalls)
	}
}

func TestStepRestartAfterInstall_RunStateDeadline(t *testing.T) {
	restoreRestartStepDurations(t)
	restartAfterInstallShutoffPoll = 5 * time.Millisecond
	restartAfterInstallShutoffMaxWait = 0
	restartAfterInstallRunningPoll = 5 * time.Millisecond
	restartAfterInstallRunningMaxWait = 35 * time.Millisecond

	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateBlocked,
				StoppedAt: time.Now(),
			}})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL:      srv.URL,
		SylveToken:    "tok",
		TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}

func TestStepRestartAfterInstall_ShutoffLoopContextCancelled(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v", got)
	}
}

func TestStepRestartAfterInstall_StartRetryContextCancelled(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var getVMCalls, startCalls int32
	var cancelOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = atomic.AddInt32(&startCalls, 1)
			cancelOnce.Do(func() {
				go func() {
					time.Sleep(2 * time.Millisecond)
					cancel()
				}()
			})
			http.Error(w, "lifecycle_task_in_progress", http.StatusConflict)
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v", got)
	}
}

func TestStepRestartAfterInstall_RunningPollGetVMIntermittentError(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			if n == 2 {
				http.Error(w, "temp", http.StatusInternalServerError)
				return
			}
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if getVMCalls < 3 {
		t.Fatalf("expected GetVM retry after transient error, getVMCalls=%d", getVMCalls)
	}
}

func TestStepRestartAfterInstall_TaskPollContextCancelled(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			vm := client.VM{
				ID:        vmID,
				RID:       vmRID,
				State:     client.DomainStateNoState,
				StoppedAt: time.Time{},
			}
			if n == 1 {
				vm.StoppedAt = time.Now()
			} else {
				vm.State = client.DomainStateRunning
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: map[string]interface{}{"t": 1}})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	go func() {
		time.Sleep(3 * time.Millisecond)
		cancel()
	}()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v", got)
	}
}

func TestStepRestartAfterInstall_RunningPollContextCancelled(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	var getVMCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMCalls, 1)
			if n == 1 {
				_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{
					ID:        vmID,
					RID:       vmRID,
					State:     client.DomainStateNoState,
					StoppedAt: time.Now(),
				}})
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{
				ID:    vmID,
				RID:   vmRID,
				State: client.DomainStateBlocked,
			}})
		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	go func() {
		time.Sleep(4 * time.Millisecond)
		cancel()
	}()

	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v", got)
	}
}
