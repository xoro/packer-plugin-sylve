// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package iso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// serveCreateVMWithNIC builds a mock Sylve API server that returns a VM with
// a non-zero network ID, exercising the NIC enable=false fix block in
// StepCreateVM.Run. detachErr and attachErr control whether the detach/attach
// endpoints return errors.
func serveCreateVMWithNIC(t *testing.T, detachErr, attachErr bool) *httptest.Server {
	t.Helper()
	var listSimpleCalls int32
	const vmRID = 7
	const vmID = 100
	const netID = 5
	macID := uint(42)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: vmRID, ID: vmID, Name: "packer-nic", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})

		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case strings.HasPrefix(r.URL.Path, "/api/vm/") && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			vm := client.VM{
				ID:    vmID,
				RID:   vmRID,
				Name:  "packer-nic",
				State: client.DomainStateRunning,
				Networks: []client.VMNetwork{{
					ID:    netID,
					MacID: &macID,
					MacObj: &client.VMNetworkObject{
						Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}},
					},
				}},
				Storages:  []client.VMStorage{{ID: 1, Type: "image", Name: "iso", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:      true,
				APIC:      true,
				StartedAt: time.Now(),
				VNCPort:   5900,
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})

		case r.URL.Path == "/api/vm/network/detach" && r.Method == http.MethodPost:
			if detachErr {
				http.Error(w, "detach failed", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case r.URL.Path == "/api/vm/network/attach" && r.Method == http.MethodPost:
			if attachErr {
				http.Error(w, "attach failed", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		default:
			http.NotFound(w, r)
		}
	}))
}

func TestStepCreateVM_Run_NICFix_DetachError(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	srv := serveCreateVMWithNIC(t, true, false)
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-nic",
		StoragePool:          "tank",
		SwitchName:           "sw",
		VNCPortMin:           port,
		VNCPortMax:           port,
		VNCHost:              "127.0.0.1",
		StorageSizeMB:        1024,
		StorageType:          "zvol",
		StorageEmulationType: "virtio-blk",
		SwitchEmulationType:  "e1000",
	}

	step := &StepCreateVM{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("iso_uuid", "iso-uuid")

	// Detach error is non-fatal; Run should still continue.
	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue; error=%v", got, state.Get("error"))
	}
}

func TestStepCreateVM_Run_NICFix_AttachError(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	srv := serveCreateVMWithNIC(t, false, true)
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-nic",
		StoragePool:          "tank",
		SwitchName:           "sw",
		VNCPortMin:           port,
		VNCPortMax:           port,
		VNCHost:              "127.0.0.1",
		StorageSizeMB:        1024,
		StorageType:          "zvol",
		StorageEmulationType: "virtio-blk",
		SwitchEmulationType:  "e1000",
	}

	step := &StepCreateVM{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("iso_uuid", "iso-uuid")

	// Attach error is non-fatal; Run should still continue.
	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue; error=%v", got, state.Get("error"))
	}
}

func TestStepCreateVM_Run_NICFix_Success(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	srv := serveCreateVMWithNIC(t, false, false)
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-nic",
		StoragePool:          "tank",
		SwitchName:           "sw",
		VNCPortMin:           port,
		VNCPortMax:           port,
		VNCHost:              "127.0.0.1",
		StorageSizeMB:        1024,
		StorageType:          "zvol",
		StorageEmulationType: "virtio-blk",
		SwitchEmulationType:  "e1000",
	}

	step := &StepCreateVM{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("iso_uuid", "iso-uuid")

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue; error=%v", got, state.Get("error"))
	}
	// Verify the NIC fix path updated vm_network_id in state.
	if netID, ok := state.Get("vm_network_id").(uint); !ok || netID == 0 {
		t.Fatalf("vm_network_id not set after NIC fix success, got %v", state.Get("vm_network_id"))
	}
	if _, ok := state.Get("vm_mac_object_id").(*uint); !ok {
		t.Fatal("vm_mac_object_id not set after NIC fix success")
	}
}

func TestStepCreateVM_Run_NICFix_RefreshFails(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)

	// Build a mock that succeeds on detach+attach but fails on the refresh
	// GetVMByRID call (third GET /api/vm/:rid). The first two GET calls succeed
	// (initial fetch + re-fetch after boot-order change); the third one fails.
	var getCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			data := []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-nic", State: client.DomainStateRunning}}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})

		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case strings.HasPrefix(r.URL.Path, "/api/vm/") && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getCalls, 1)
			if n >= 3 {
				// Third call is the refresh after NIC reattach — make it return
				// an empty network list so the inner if body is skipped.
				vm := client.VM{
					ID: 100, RID: 7, Name: "packer-nic", State: client.DomainStateRunning,
					Networks: nil,
					Storages: []client.VMStorage{{ID: 1, Type: "image", Name: "iso", Emulation: "ahci-cd"}},
				}
				_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
				return
			}
			macID := uint(42)
			vm := client.VM{
				ID: 100, RID: 7, Name: "packer-nic", State: client.DomainStateRunning,
				Networks: []client.VMNetwork{{
					ID: 5, MacID: &macID,
					MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}},
				}},
				Storages:  []client.VMStorage{{ID: 1, Type: "image", Name: "iso", Emulation: "ahci-cd"}},
				StartedAt: time.Now(), VNCPort: 5900,
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})

		case r.URL.Path == "/api/vm/network/detach" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case r.URL.Path == "/api/vm/network/attach" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})

		default:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
		VMName: "packer-nic", StoragePool: "tank", SwitchName: "sw",
		VNCPortMin: port, VNCPortMax: port, VNCHost: "127.0.0.1",
		StorageSizeMB: 1024, StorageType: "zvol",
		StorageEmulationType: "virtio-blk", SwitchEmulationType: "e1000",
	}

	step := &StepCreateVM{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("iso_uuid", "iso-uuid")

	// Should still succeed even when the refresh returns no networks.
	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v; error=%v", got, state.Get("error"))
	}
}
