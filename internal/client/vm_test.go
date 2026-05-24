// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serveVM starts a handler that responds to a single path/method combo.
func serveVM(t *testing.T, path, method string, respond func(w http.ResponseWriter, r *http.Request)) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != path || r.Method != method {
			http.NotFound(w, r)
			return
		}
		respond(w, r)
	}))
	c := New(srv.URL, "tok", false)
	return c, srv
}

func okJSON(w http.ResponseWriter, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// ---------------------------------------------------------------------------
// VMNetwork.MACAddress
// ---------------------------------------------------------------------------

func TestMACAddress_TopLevelMAC(t *testing.T) {
	n := VMNetwork{MAC: "aa:bb:cc:dd:ee:ff"}
	if got := n.MACAddress(); got != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MACAddress = %q, want %q", got, "aa:bb:cc:dd:ee:ff")
	}
}

func TestMACAddress_MacObj(t *testing.T) {
	n := VMNetwork{
		MacObj: &VMNetworkObject{
			Entries: []VMNetworkObjectEntry{{Value: "11:22:33:44:55:66"}},
		},
	}
	if got := n.MACAddress(); got != "11:22:33:44:55:66" {
		t.Errorf("MACAddress = %q, want %q", got, "11:22:33:44:55:66")
	}
}

func TestMACAddress_TopLevelPreferredOverMacObj(t *testing.T) {
	n := VMNetwork{
		MAC: "aa:bb:cc:dd:ee:ff",
		MacObj: &VMNetworkObject{
			Entries: []VMNetworkObjectEntry{{Value: "11:22:33:44:55:66"}},
		},
	}
	if got := n.MACAddress(); got != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MACAddress = %q; top-level MAC should win", got)
	}
}

func TestMACAddress_NilMacObj(t *testing.T) {
	n := VMNetwork{MacObj: nil}
	if got := n.MACAddress(); got != "" {
		t.Errorf("MACAddress = %q, want empty when MacObj is nil", got)
	}
}

func TestMACAddress_EmptyMacObjEntries(t *testing.T) {
	n := VMNetwork{MacObj: &VMNetworkObject{Entries: []VMNetworkObjectEntry{}}}
	if got := n.MACAddress(); got != "" {
		t.Errorf("MACAddress = %q, want empty for empty Entries", got)
	}
}

// ---------------------------------------------------------------------------
// DomainState constants
// ---------------------------------------------------------------------------

func TestDomainState_Constants(t *testing.T) {
	cases := []struct {
		name string
		got  DomainState
		want DomainState
	}{
		{"NoState", DomainStateNoState, 0},
		{"Running", DomainStateRunning, 1},
		{"Blocked", DomainStateBlocked, 2},
		{"Paused", DomainStatePaused, 3},
		{"Shutdown", DomainStateShutdown, 4},
		{"Shutoff", DomainStateShutoff, 5},
		{"Crashed", DomainStateCrashed, 6},
		{"PMSuspended", DomainStatePMSuspended, 7},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("DomainState%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// CreateVM
// ---------------------------------------------------------------------------

func TestCreateVM_Success(t *testing.T) {
	var gotName string
	c, srv := serveVM(t, "/api/vm", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		var req CreateVMRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotName = req.Name
		okJSON(w, APIResponse[interface{}]{Status: "ok"})
	})
	defer srv.Close()

	rid := uint(42)
	err := c.CreateVM(CreateVMRequest{Name: "packer-vm", RID: &rid})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotName != "packer-vm" {
		t.Errorf("VM name = %q, want %q", gotName, "packer-vm")
	}
}

func TestCreateVM_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	})
	defer srv.Close()

	rid := uint(1)
	if err := c.CreateVM(CreateVMRequest{Name: "vm", RID: &rid}); err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetVMByRID
// ---------------------------------------------------------------------------

func TestGetVMByRID_Success(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/7?type=rid", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, APIResponse[VM]{Data: VM{ID: 10, RID: 7, Name: "my-vm"}})
	})
	defer srv.Close()

	vm, err := c.GetVMByRID(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vm.RID != 7 || vm.Name != "my-vm" {
		t.Errorf("unexpected VM: %+v", vm)
	}
}

func TestGetVMByRID_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/99?type=rid", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	defer srv.Close()

	if _, err := c.GetVMByRID(99); err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListVMsSimple
// ---------------------------------------------------------------------------

func TestListVMsSimple_Success(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/simple", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, APIResponse[[]SimpleVM]{Data: []SimpleVM{
			{RID: 1, Name: "vm-a", VNCPort: 5900},
			{RID: 2, Name: "vm-b", VNCPort: 5901},
		}})
	})
	defer srv.Close()

	vms, err := c.ListVMsSimple()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vms) != 2 || vms[0].Name != "vm-a" {
		t.Errorf("unexpected VMs: %+v", vms)
	}
}

func TestListVMsSimple_Empty(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/simple", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, APIResponse[[]SimpleVM]{Data: []SimpleVM{}})
	})
	defer srv.Close()

	vms, err := c.ListVMsSimple()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vms) != 0 {
		t.Errorf("expected empty list, got %v", vms)
	}
}

// ---------------------------------------------------------------------------
// GetSimpleVMByRID
// ---------------------------------------------------------------------------

func TestGetSimpleVMByRID_Success(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/simple/3?type=rid", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, APIResponse[SimpleVM]{Data: SimpleVM{RID: 3, State: DomainStateRunning}})
	})
	defer srv.Close()

	vm, err := c.GetSimpleVMByRID(3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vm.RID != 3 || vm.State != DomainStateRunning {
		t.Errorf("unexpected simple VM: %+v", vm)
	}
}

func TestGetSimpleVMByRID_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/simple/99?type=rid", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	defer srv.Close()

	if _, err := c.GetSimpleVMByRID(99); err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// StartVM
// ---------------------------------------------------------------------------

func TestStartVM_Success(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/start/5", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, APIResponse[interface{}]{Status: "ok"})
	})
	defer srv.Close()

	if err := c.StartVM(5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartVM_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/start/5", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	})
	defer srv.Close()

	if err := c.StartVM(5); err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
}

// ---------------------------------------------------------------------------
// StopVM
// ---------------------------------------------------------------------------

func TestStopVM_Success(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/stop/6", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, APIResponse[interface{}]{Status: "ok"})
	})
	defer srv.Close()

	if err := c.StopVM(6); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStopVM_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/stop/6", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	defer srv.Close()

	if err := c.StopVM(6); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetVMLogs
// ---------------------------------------------------------------------------

func TestGetVMLogs_Success(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/logs/8", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok","data":{"logs":"bhyve started\nbhyve exited"}}`)
	})
	defer srv.Close()

	logs, err := c.GetVMLogs(8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(logs, "bhyve") {
		t.Errorf("logs = %q, want it to contain 'bhyve'", logs)
	}
}

func TestGetVMLogs_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/logs/8", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	defer srv.Close()

	if _, err := c.GetVMLogs(8); err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// UpdateStorageBootOrder
// ---------------------------------------------------------------------------

func TestUpdateStorageBootOrder_Success(t *testing.T) {
	var gotReq StorageUpdateRequest
	c, srv := serveVM(t, "/api/vm/storage/update", http.MethodPut, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		okJSON(w, APIResponse[interface{}]{Status: "ok"})
	})
	defer srv.Close()

	if err := c.UpdateStorageBootOrder(12, "iso", "ahci-cd", 100); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.ID != 12 || gotReq.Name != "iso" || gotReq.Emulation != "ahci-cd" {
		t.Errorf("unexpected request: %+v", gotReq)
	}
	if gotReq.BootOrder == nil || *gotReq.BootOrder != 100 {
		t.Errorf("BootOrder = %v, want 100", gotReq.BootOrder)
	}
}

func TestUpdateStorageBootOrder_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/storage/update", http.MethodPut, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	})
	defer srv.Close()

	if err := c.UpdateStorageBootOrder(1, "d", "e", 0); err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// DisableISOStorage
// ---------------------------------------------------------------------------

func TestDisableISOStorage_Success(t *testing.T) {
	var gotReq StorageUpdateRequest
	c, srv := serveVM(t, "/api/vm/storage/update", http.MethodPut, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		okJSON(w, APIResponse[interface{}]{Status: "ok"})
	})
	defer srv.Close()

	if err := c.DisableISOStorage(7, "cd0", "ahci-cd"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.Enable == nil || *gotReq.Enable != false {
		t.Errorf("Enable = %v, want false", gotReq.Enable)
	}
	if gotReq.ID != 7 {
		t.Errorf("storage ID = %d, want 7", gotReq.ID)
	}
}

func TestDisableISOStorage_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/storage/update", http.MethodPut, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	defer srv.Close()

	if err := c.DisableISOStorage(1, "iso", "ahci-cd"); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// DisableStartAtBoot
// ---------------------------------------------------------------------------

func TestDisableStartAtBoot_Success(t *testing.T) {
	path := fmt.Sprintf("/api/vm/options/boot-order/%d", 9)
	c, srv := serveVM(t, path, http.MethodPut, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, APIResponse[interface{}]{Status: "ok"})
	})
	defer srv.Close()

	if err := c.DisableStartAtBoot(9); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDisableStartAtBoot_Error(t *testing.T) {
	path := fmt.Sprintf("/api/vm/options/boot-order/%d", 9)
	c, srv := serveVM(t, path, http.MethodPut, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	defer srv.Close()

	if err := c.DisableStartAtBoot(9); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// HasActiveLifecycleTask
// ---------------------------------------------------------------------------

func TestHasActiveLifecycleTask_Active(t *testing.T) {
	path := fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", 4)
	c, srv := serveVM(t, path, http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		// Non-nil data map means a task is active.
		okJSON(w, APIResponse[map[string]interface{}]{
			Data: map[string]interface{}{"id": 1},
		})
	})
	defer srv.Close()

	active, err := c.HasActiveLifecycleTask(4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !active {
		t.Error("expected active=true when data is non-nil")
	}
}

func TestHasActiveLifecycleTask_None(t *testing.T) {
	path := fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", 4)
	c, srv := serveVM(t, path, http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		// Null data means no active task.
		okJSON(w, APIResponse[map[string]interface{}]{Data: nil})
	})
	defer srv.Close()

	active, err := c.HasActiveLifecycleTask(4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if active {
		t.Error("expected active=false when data is nil")
	}
}

func TestHasActiveLifecycleTask_Error(t *testing.T) {
	path := fmt.Sprintf("/api/tasks/lifecycle/active/vm/%d", 4)
	c, srv := serveVM(t, path, http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	defer srv.Close()

	if _, err := c.HasActiveLifecycleTask(4); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// DeleteVM
// ---------------------------------------------------------------------------

func TestDeleteVM_Success(t *testing.T) {
	path := fmt.Sprintf("/api/vm/%d?deletemacs=true&deleterawdisks=true&deletevolumes=true", 11)
	c, srv := serveVM(t, path, http.MethodDelete, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	defer srv.Close()

	if err := c.DeleteVM(11); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteVM_Error(t *testing.T) {
	path := fmt.Sprintf("/api/vm/%d?deletemacs=true&deleterawdisks=true&deletevolumes=true", 11)
	c, srv := serveVM(t, path, http.MethodDelete, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	defer srv.Close()

	if err := c.DeleteVM(11); err == nil {
		t.Fatal("expected error for 404 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListVMsSimple error path
// ---------------------------------------------------------------------------

func TestListVMsSimple_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/simple", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	defer srv.Close()

	if _, err := c.ListVMsSimple(); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// FindVMByName
// ---------------------------------------------------------------------------

func TestFindVMByName_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.RequestURI() == "/api/vm/simple":
			okJSON(w, APIResponse[[]SimpleVM]{Data: []SimpleVM{
				{RID: 3, ID: 10, Name: "my-vm"},
				{RID: 4, ID: 11, Name: "other-vm"},
			}})
		case r.Method == http.MethodGet && r.URL.RequestURI() == "/api/vm/3?type=rid":
			okJSON(w, APIResponse[VM]{Data: VM{ID: 10, RID: 3, Name: "my-vm",
				Storages: []VMStorage{{ID: 1, Type: VMStorageTypeZVol, Name: "disk0"}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	vm, err := c.FindVMByName("my-vm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vm.RID != 3 || vm.Name != "my-vm" {
		t.Errorf("unexpected VM: %+v", vm)
	}
}

func TestFindVMByName_NotFound(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/simple", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, APIResponse[[]SimpleVM]{Data: []SimpleVM{
			{RID: 1, Name: "other-vm"},
		}})
	})
	defer srv.Close()

	_, err := c.FindVMByName("missing-vm")
	if err == nil {
		t.Fatal("expected error for missing VM, got nil")
	}
	if !strings.Contains(err.Error(), "VM not found") {
		t.Errorf("error = %q, want to contain \"VM not found\"", err.Error())
	}
}

func TestFindVMByName_ListError(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/simple", http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	defer srv.Close()

	if _, err := c.FindVMByName("any-vm"); err == nil {
		t.Fatal("expected error when list fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// DetachVMNetwork
// ---------------------------------------------------------------------------

func TestDetachVMNetwork_Success(t *testing.T) {
	var gotReq NetworkDetachRequest
	c, srv := serveVM(t, "/api/vm/network/detach", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		okJSON(w, APIResponse[interface{}]{Status: "ok"})
	})
	defer srv.Close()

	if err := c.DetachVMNetwork(5, 12); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.RID != 5 || gotReq.NetworkID != 12 {
		t.Errorf("unexpected request body: RID=%d NetworkID=%d, want RID=5 NetworkID=12",
			gotReq.RID, gotReq.NetworkID)
	}
}

func TestDetachVMNetwork_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/network/detach", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	defer srv.Close()

	if err := c.DetachVMNetwork(1, 2); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// ReattachVMNetwork
// ---------------------------------------------------------------------------

func TestReattachVMNetwork_Success(t *testing.T) {
	var gotReq NetworkAttachRequest
	c, srv := serveVM(t, "/api/vm/network/attach", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		okJSON(w, APIResponse[interface{}]{Status: "ok"})
	})
	defer srv.Close()

	macID := uint(99)
	if err := c.ReattachVMNetwork(5, "PackerSwitch", "virtio-net", &macID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.RID != 5 || gotReq.SwitchName != "PackerSwitch" || gotReq.Emulation != "virtio-net" {
		t.Errorf("unexpected request: %+v", gotReq)
	}
	if gotReq.MacID == nil || *gotReq.MacID != 99 {
		t.Errorf("MacID = %v, want 99", gotReq.MacID)
	}
}

func TestReattachVMNetwork_NilMacID(t *testing.T) {
	var gotReq NetworkAttachRequest
	c, srv := serveVM(t, "/api/vm/network/attach", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		okJSON(w, APIResponse[interface{}]{Status: "ok"})
	})
	defer srv.Close()

	if err := c.ReattachVMNetwork(7, "mySwitch", "e1000", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.MacID != nil {
		t.Errorf("MacID = %v, want nil (omitted)", gotReq.MacID)
	}
}

func TestReattachVMNetwork_Error(t *testing.T) {
	c, srv := serveVM(t, "/api/vm/network/attach", http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	})
	defer srv.Close()

	if err := c.ReattachVMNetwork(1, "sw", "virtio-net", nil); err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
}
