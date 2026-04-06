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

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func restoreCreateVMDurations(t *testing.T) {
	t.Helper()
	orig := createVMPollInterval
	t.Cleanup(func() { createVMPollInterval = orig })
	createVMPollInterval = 1 * time.Millisecond
}

// newFreeTCPPort returns a free 127.0.0.1 TCP port for VNC selection tests.
func newFreeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// twoConsecutiveFreeTCPPorts returns two adjacent free 127.0.0.1 TCP ports
// so selectVNCPort can be exercised when Sylve already claims the lower port.
func twoConsecutiveFreeTCPPorts(t *testing.T) (lo, hi int) {
	t.Helper()
	for p := 20000; p < 60000; p += 2 {
		l1, err1 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err1 != nil {
			continue
		}
		l2, err2 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p+1))
		if err2 != nil {
			l1.Close()
			continue
		}
		l1.Close()
		l2.Close()
		return p, p + 1
	}
	t.Fatal("could not find two consecutive free TCP ports")
	panic("unreachable")
}

// serveCreateVMFlow runs a minimal Sylve API for StepCreateVM.Run success.
func serveCreateVMFlow(t *testing.T, vmName string, vmRID int, vmID int) *httptest.Server {
	t.Helper()
	var listSimpleCalls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: uint(vmRID), ID: uint(vmID), Name: vmName, State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/") && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/vm/"), "/")
			if len(parts) < 1 || parts[0] == "" {
				http.NotFound(w, r)
				return
			}
			if parts[0] != fmt.Sprint(vmRID) {
				http.NotFound(w, r)
				return
			}
			vm := client.VM{
				ID:            uint(vmID),
				RID:           uint(vmRID),
				Name:          vmName,
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestStepCreateVM_Run_UpdateBootOrderWarning(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listSimpleCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/7" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			vm := client.VM{
				ID:            100,
				RID:           7,
				Name:          "packer-test",
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			http.Error(w, "no", http.StatusInternalServerError)
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
}

func TestStepCreateVM_Run_Success(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	srv := serveCreateVMFlow(t, "packer-test", 7, 100)
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if state.Get("vm_rid").(uint) != 7 {
		t.Fatalf("vm_rid = %v", state.Get("vm_rid"))
	}
	if state.Get("iso_storage_id").(int) != 1 {
		t.Fatalf("iso_storage_id = %v", state.Get("iso_storage_id"))
	}
}

func TestStepCreateVM_Run_SuccessWithNoNetworks(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listSimpleCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/") && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			vm := client.VM{
				ID:        100,
				RID:       7,
				Name:      "packer-test",
				State:     client.DomainStateRunning,
				Networks:  nil,
				Storages:  []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:      true,
				APIC:      true,
				StartedAt: time.Now(),
				VNCPort:   5900,
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if state.Get("vm_mac").(string) != "" {
		t.Fatalf("expected empty vm_mac when VM has no networks, got %q", state.Get("vm_mac"))
	}
}

func TestStepCreateVM_Run_CreateVMFails(t *testing.T) {
	port := newFreeTCPPort(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: nil})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			http.Error(w, "bad", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}

func TestStepCreateVM_Run_GetVMByRIDFails(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listSimpleCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/") && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			http.Error(w, "missing", http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}

func TestStepCreateVM_Run_RIDRetryThenSuccess(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listSimpleCalls, postCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			n := atomic.AddInt32(&postCalls, 1)
			if n == 1 {
				http.Error(w, `rid_or_name_already_in_use`, http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/7" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			vm := client.VM{
				ID:            100,
				RID:           7,
				Name:          "packer-test",
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if postCalls < 2 {
		t.Fatalf("expected POST retry, postCalls=%d", postCalls)
	}
}

func TestStepCreateVM_Run_ContextCancelledDuringWaitForVM(t *testing.T) {
	port := newFreeTCPPort(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: nil})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(1500 * time.Millisecond)
		cancel()
	}()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}

func TestStepCreateVM_Run_RandReadFails(t *testing.T) {
	port := newFreeTCPPort(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: []client.SimpleVM{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldRand := randReadFn
	randReadFn = func([]byte) (int, error) { return 0, fmt.Errorf("simulated entropy failure") }
	t.Cleanup(func() { randReadFn = oldRand })

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	err, ok := state.Get("error").(error)
	if !ok || !strings.Contains(err.Error(), "generate random RID") {
		t.Fatalf("error = %v, want generate random RID failure", state.Get("error"))
	}
}

func TestStepCreateVM_Run_VMNotFoundInSimpleListAfterDeadline(t *testing.T) {
	restoreCreateVMDurations(t)
	oldDeadline := createVMListDeadline
	createVMListDeadline = 5 * time.Millisecond
	t.Cleanup(func() { createVMListDeadline = oldDeadline })

	port := newFreeTCPPort(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: []client.SimpleVM{}})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "never-listed",
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

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	err, ok := state.Get("error").(error)
	if !ok || !strings.Contains(err.Error(), "not found in Sylve after creation") {
		t.Fatalf("error = %v, want VM not found after creation", state.Get("error"))
	}
}

func TestStepCreateVM_Run_ListVMsSimpleTransientErrorsDuringPoll(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listCalls, 1)
			if n == 2 || n == 3 {
				http.Error(w, "temporary list failure", http.StatusInternalServerError)
				return
			}
			var data []client.SimpleVM
			if n >= 4 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/") && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/vm/"), "/")
			if len(parts) < 1 || parts[0] == "" {
				http.NotFound(w, r)
				return
			}
			if parts[0] != "7" {
				http.NotFound(w, r)
				return
			}
			vm := client.VM{
				ID:            100,
				RID:           7,
				Name:          "packer-test",
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if listCalls < 4 {
		t.Fatalf("expected at least 4 list VM calls, got %d", listCalls)
	}
}

func TestStepCreateVM_Run_GetVMByRIDRefetchAfterDisableStartAtBootFails(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listSimpleCalls, getVMByRIDCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/") && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMByRIDCalls, 1)
			if n == 2 {
				http.Error(w, "refetch after boot-order changes failed", http.StatusInternalServerError)
				return
			}
			vm := client.VM{
				ID:            100,
				RID:           7,
				Name:          "packer-test",
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "cdrom", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if atomic.LoadInt32(&getVMByRIDCalls) != 2 {
		t.Fatalf("expected 2 GetVMByRID calls, got %d", getVMByRIDCalls)
	}
	if name := state.Get("iso_storage_name"); name != "cdrom" {
		t.Fatalf("iso_storage_name = %v, want cdrom", name)
	}
}

func TestStepCreateVM_Run_StaleArtifactsRetryThenSuccess(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listSimpleCalls, postCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			n := atomic.AddInt32(&postCalls, 1)
			if n == 1 {
				http.Error(w, `vm_create_stale_artifacts_detected`, http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/7" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			vm := client.VM{
				ID:            100,
				RID:           7,
				Name:          "packer-test",
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
}

// TestStepCreateVM_Run_ListVMSimpleErrorThenSuccess covers the ListVMsSimple
// error branch in the post-create poll loop: transient failures are ignored and
// the step continues once the VM appears.
func TestStepCreateVM_Run_ListVMSimpleErrorThenSuccess(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listSimpleCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			if n == 1 {
				// selectVNCPort lists VMs before CreateVM; must succeed.
				_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: nil})
				return
			}
			if n == 2 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			data := []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/7" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			vm := client.VM{
				ID:            100,
				RID:           7,
				Name:          "packer-test",
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if listSimpleCalls < 3 {
		t.Fatalf("expected ListVMsSimple retry after error, listSimpleCalls=%d", listSimpleCalls)
	}
}

// TestStepCreateVM_Run_DisableStartAtBootWarning covers DisableStartAtBoot
// failure: the step logs a warning and still completes.
func TestStepCreateVM_Run_DisableStartAtBootWarning(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listSimpleCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/7" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			vm := client.VM{
				ID:            100,
				RID:           7,
				Name:          "packer-test",
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			http.Error(w, "disable startAtBoot failed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
}

// TestStepCreateVM_Run_RefetchVMFailsAfterBootOrder covers the non-fatal path
// where GetVMByRID after DisableStartAtBoot fails: the step still completes with
// the prior vm snapshot.
func TestStepCreateVM_Run_RefetchVMFailsAfterBootOrder(t *testing.T) {
	restoreCreateVMDurations(t)
	port := newFreeTCPPort(t)
	var listSimpleCalls, getVMByRIDCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n >= 2 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/7" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			n := atomic.AddInt32(&getVMByRIDCalls, 1)
			if n >= 2 {
				http.Error(w, "gone", http.StatusNotFound)
				return
			}
			vm := client.VM{
				ID:            100,
				RID:           7,
				Name:          "packer-test",
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if getVMByRIDCalls < 2 {
		t.Fatalf("expected second GetVMByRID for refetch, getVMByRIDCalls=%d", getVMByRIDCalls)
	}
}

// TestStepCreateVM_Run_SkipsVNCPortWhenFirstPortClaimedInSylve covers
// selectVNCPort when ListVMsSimple reports another VM already using the first
// port in [VNCPortMin, VNCPortMax]: the step must bind the next free port.
func TestStepCreateVM_Run_SkipsVNCPortWhenFirstPortClaimedInSylve(t *testing.T) {
	restoreCreateVMDurations(t)
	portLo, portHi := twoConsecutiveFreeTCPPorts(t)
	var listSimpleCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/vm/simple" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&listSimpleCalls, 1)
			var data []client.SimpleVM
			if n == 1 {
				data = []client.SimpleVM{{VNCPort: portLo}}
			} else if n >= 2 {
				data = []client.SimpleVM{{RID: 7, ID: 100, Name: "packer-test", State: client.DomainStateRunning}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.SimpleVM]{Status: "ok", Data: data})
		case r.URL.Path == "/api/vm" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case r.URL.Path == "/api/vm/7" && r.Method == http.MethodGet && strings.Contains(r.URL.RawQuery, "type=rid"):
			vm := client.VM{
				ID:            100,
				RID:           7,
				Name:          "packer-test",
				State:         client.DomainStateRunning,
				Networks:      []client.VMNetwork{{MacObj: &client.VMNetworkObject{Entries: []client.VMNetworkObjectEntry{{Value: "aa:bb:cc:dd:ee:ff"}}}}},
				Storages:      []client.VMStorage{{ID: 1, Type: "image", Name: "", Emulation: "ahci-cd", BootOrder: 0}},
				ACPI:          true,
				APIC:          true,
				StartedAt:     time.Now(),
				VNCPort:       5900,
				VNCResolution: "1024x768",
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.VM]{Status: "ok", Data: vm})
		case r.URL.Path == "/api/vm/storage/update" && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		case strings.HasPrefix(r.URL.Path, "/api/vm/options/boot-order/") && r.Method == http.MethodPut:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:             srv.URL,
		SylveToken:           "tok",
		TLSSkipVerify:        true,
		VMName:               "packer-test",
		StoragePool:          "tank",
		SwitchName:           "sw",
		VNCPortMin:           portLo,
		VNCPortMax:           portHi,
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
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if listSimpleCalls < 2 {
		t.Fatalf("expected post-create ListVMsSimple poll, listSimpleCalls=%d", listSimpleCalls)
	}
	if cfg.VNCPort != portHi {
		t.Fatalf("VNCPort = %d, want %d (skipped Sylve-claimed %d)", cfg.VNCPort, portHi, portLo)
	}
}

func TestStepCreateVM_Cleanup_DeleteVMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/vm/") {
			http.Error(w, "no", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepCreateVM{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}, vmRID: 77}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_rid", uint(77))
	state.Put(multistep.StateHalted, true)

	step.Cleanup(state)
}
