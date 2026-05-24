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
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// serveRestartWithNIC builds a mock Sylve API server for
// StepRestartAfterInstall.Run that handles the NIC detach/reattach endpoints.
func serveRestartWithNIC(t *testing.T, vmRID, vmID int, detachErr, attachErr bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == fmt.Sprintf("/api/vm/stop/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case path == fmt.Sprintf("/api/vm/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{
				Status: "ok", Data: client.VM{ID: uint(vmID), RID: uint(vmRID), StoppedAt: time.Now()},
			})

		case path == fmt.Sprintf("/api/vm/simple/%d", vmRID) && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.SimpleVM]{
				Status: "ok", Data: client.SimpleVM{ID: uint(vmID), RID: uint(vmRID), State: client.DomainStateRunning},
			})

		case path == fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", vmID) && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[map[string]interface{}]{Status: "ok", Data: nil})

		case path == fmt.Sprintf("/api/vm/start/%d", vmRID) && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case path == "/api/vm/network/detach" && r.Method == http.MethodPost:
			if detachErr {
				http.Error(w, "detach failed", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case path == "/api/vm/network/attach" && r.Method == http.MethodPost:
			if attachErr {
				http.Error(w, "attach failed", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		default:
			http.NotFound(w, r)
		}
	}))
}

func TestStepRestartAfterInstall_NICFix_DetachError(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := serveRestartWithNIC(t, vmRID, vmID, true, false)
	defer srv.Close()

	macObjID := uint(42)
	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
		SwitchName: "sw", SwitchEmulationType: "e1000",
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)
	state.Put("vm_network_id", uint(5))
	state.Put("vm_mac_object_id", &macObjID)

	// Detach error is non-fatal; Run should continue.
	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v; error=%v", got, state.Get("error"))
	}
}

func TestStepRestartAfterInstall_NICFix_AttachError(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := serveRestartWithNIC(t, vmRID, vmID, false, true)
	defer srv.Close()

	macObjID := uint(42)
	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
		SwitchName: "sw", SwitchEmulationType: "e1000",
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)
	state.Put("vm_network_id", uint(5))
	state.Put("vm_mac_object_id", &macObjID)

	// Attach error is non-fatal; Run should continue.
	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v; error=%v", got, state.Get("error"))
	}
}

func TestStepRestartAfterInstall_NICFix_Success(t *testing.T) {
	restoreRestartStepDurations(t)
	const vmRID = 9
	const vmID = 100

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv := serveRestartWithNIC(t, vmRID, vmID, false, false)
	defer srv.Close()

	macObjID := uint(42)
	step := &StepRestartAfterInstall{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
		SwitchName: "sw", SwitchEmulationType: "e1000",
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(vmRID))
	state.Put("vm_id", uint(vmID))
	state.Put("iso_storage_id", 1)
	state.Put("iso_storage_name", "iso")
	state.Put("iso_storage_emulation", "ahci-cd")
	state.Put("vnc_view_listener", ln)
	state.Put("vm_network_id", uint(5))
	state.Put("vm_mac_object_id", &macObjID)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v; error=%v", got, state.Get("error"))
	}
}
