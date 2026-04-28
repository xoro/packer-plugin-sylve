// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/json"
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

func restoreDownloadISODurations(t *testing.T) {
	t.Helper()
	orig := downloadISOPollInterval
	t.Cleanup(func() { downloadISOPollInterval = orig })
	downloadISOPollInterval = 1 * time.Millisecond
}

func restoreDownloadISOTotalTimeout(t *testing.T) {
	t.Helper()
	orig := downloadISOTotalTimeout
	t.Cleanup(func() { downloadISOTotalTimeout = orig })
	downloadISOTotalTimeout = 30 * time.Millisecond
}

// Covers StepDownloadISO Run loop branch: FindDownloadByURL returns error (continue).
func TestStepDownloadISO_ListErrorThenDone(t *testing.T) {
	restoreDownloadISODurations(t)
	const isoURL = "https://example.com/list-flap.iso"
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			if n == 1 {
				_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: nil})
				return
			}
			if n == 2 {
				http.Error(w, "temporary", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{
				Status: "ok",
				Data: []client.Download{{
					URL:    isoURL,
					Status: client.DownloadStatusDone,
					UUID:   "after-flap",
				}},
			})
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

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("got %v", got)
	}
	if state.Get("iso_uuid") != "after-flap" {
		t.Fatalf("iso_uuid = %v", state.Get("iso_uuid"))
	}
}

func TestStepShutdown_StopVMErrorStillContinues(t *testing.T) {
	restoreShutdownDurations(t)
	const rid = uint(3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case p == "/api/vm/stop/3" && r.Method == http.MethodPost:
			http.Error(w, "already", http.StatusConflict)
		case p == "/api/vm/simple/3" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
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

func TestStepRestartAfterInstall_DisableISOErrorStillContinues(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 11
	const vmID = 110

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
		case path == "/api/vm/stop/11" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/11" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = atomic.AddInt32(&getVMCalls, 1)
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: client.VM{ID: vmID, RID: vmRID, State: client.DomainStateNoState, StoppedAt: time.Now()}})
		case path == "/api/vm/simple/11" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{Status: "ok", Data: client.SimpleVM{ID: vmID, RID: vmRID, State: client.DomainStateRunning}})
		case path == "/api/tasks/lifecycle/active/vm/110" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})
		case path == "/api/vm/start/11" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			http.Error(w, "no", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepRestartAfterInstall{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
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

// ---------------------------------------------------------------------------
// Cleanup no-ops
// ---------------------------------------------------------------------------

func TestStepDeleteVM_Cleanup_IsNoop(t *testing.T) {
	step := &StepDeleteVM{}
	step.Cleanup(new(multistep.BasicStateBag))
}

func TestStepDiscoverIP_Cleanup_IsNoop(t *testing.T) {
	step := &StepDiscoverIP{}
	step.Cleanup(new(multistep.BasicStateBag))
}

func TestStepDownloadISO_Cleanup_IsNoop(t *testing.T) {
	step := &StepDownloadISO{}
	step.Cleanup(new(multistep.BasicStateBag))
}

func TestStepRestartAfterInstall_Cleanup_IsNoop(t *testing.T) {
	step := &StepRestartAfterInstall{}
	step.Cleanup(new(multistep.BasicStateBag))
}

func TestStepShutdown_Cleanup_IsNoop(t *testing.T) {
	step := &StepShutdown{}
	step.Cleanup(new(multistep.BasicStateBag))
}

func TestStepStartVM_Cleanup_IsNoop(t *testing.T) {
	step := &StepStartVM{}
	step.Cleanup(new(multistep.BasicStateBag))
}

func TestStepVNCBootCommand_Cleanup_IsNoop(t *testing.T) {
	step := &StepVNCBootCommand{}
	step.Cleanup(new(multistep.BasicStateBag))
}
