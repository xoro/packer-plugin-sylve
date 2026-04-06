// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveDHCPLeases starts an httptest server that returns the given leases from
// GET /api/network/dhcp/lease and returns a Client pointed at it.
func serveDHCPLeases(t *testing.T, leases []FileLease) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/network/dhcp/lease" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[Leases]{
			Status: "ok",
			Data:   Leases{File: leases},
		})
	}))
	c := New(srv.URL, "tok", true)
	return c, srv
}

// ---------------------------------------------------------------------------
// FindLeaseByMAC
// ---------------------------------------------------------------------------

func TestFindLeaseByMAC_Found(t *testing.T) {
	c, srv := serveDHCPLeases(t, []FileLease{
		{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.100"},
		{MAC: "11:22:33:44:55:66", IP: "192.168.1.101"},
	})
	defer srv.Close()

	lease, err := c.FindLeaseByMAC("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lease == nil {
		t.Fatal("FindLeaseByMAC: expected a match, got nil")
	}
	if lease.IP != "192.168.1.100" {
		t.Errorf("IP = %q, want %q", lease.IP, "192.168.1.100")
	}
}

func TestFindLeaseByMAC_CaseInsensitive(t *testing.T) {
	c, srv := serveDHCPLeases(t, []FileLease{
		{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.100"},
	})
	defer srv.Close()

	// Lowercase query should match uppercase stored MAC.
	lease, err := c.FindLeaseByMAC("aa:bb:cc:dd:ee:ff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lease == nil {
		t.Fatal("FindLeaseByMAC: expected case-insensitive match, got nil")
	}
}

func TestFindLeaseByMAC_NotFound(t *testing.T) {
	c, srv := serveDHCPLeases(t, []FileLease{
		{MAC: "AA:BB:CC:DD:EE:FF", IP: "192.168.1.100"},
	})
	defer srv.Close()

	lease, err := c.FindLeaseByMAC("00:11:22:33:44:55")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lease != nil {
		t.Errorf("expected nil for unknown MAC, got %+v", lease)
	}
}

func TestFindLeaseByMAC_EmptyList(t *testing.T) {
	c, srv := serveDHCPLeases(t, []FileLease{})
	defer srv.Close()

	lease, err := c.FindLeaseByMAC("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lease != nil {
		t.Errorf("expected nil for empty lease list, got %+v", lease)
	}
}

func TestFindLeaseByMAC_FirstMatchReturned(t *testing.T) {
	// If the same MAC appears twice, the first entry should win.
	c, srv := serveDHCPLeases(t, []FileLease{
		{MAC: "AA:BB:CC:DD:EE:FF", IP: "10.0.0.1"},
		{MAC: "AA:BB:CC:DD:EE:FF", IP: "10.0.0.2"},
	})
	defer srv.Close()

	lease, err := c.FindLeaseByMAC("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lease == nil {
		t.Fatal("expected a match, got nil")
	}
	if lease.IP != "10.0.0.1" {
		t.Errorf("IP = %q, want first entry %q", lease.IP, "10.0.0.1")
	}
}

// ---------------------------------------------------------------------------
// GetBasicSettings
// ---------------------------------------------------------------------------

func TestGetBasicSettings_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/basic/settings" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[BasicSettings]{
			Status: "ok",
			Data: BasicSettings{
				Initialized: true,
				Pools:       []string{"tank"},
				Services:    []string{"nginx"},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	settings, err := c.GetBasicSettings()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !settings.Initialized {
		t.Error("expected Initialized = true")
	}
	if len(settings.Pools) != 1 || settings.Pools[0] != "tank" {
		t.Errorf("unexpected Pools: %v", settings.Pools)
	}
}

func TestGetBasicSettings_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if _, err := c.GetBasicSettings(); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListSwitches
// ---------------------------------------------------------------------------

func TestListSwitches_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/network/switch" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[SwitchList]{
			Status: "ok",
			Data: SwitchList{
				Standard: []StandardSwitch{{ID: 1, Name: "vmbr0"}},
				Manual:   []ManualSwitch{},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	list, err := c.ListSwitches()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Standard) != 1 || list.Standard[0].Name != "vmbr0" {
		t.Errorf("unexpected switches: %+v", list)
	}
}

func TestListSwitches_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[SwitchList]{
			Data: SwitchList{Standard: []StandardSwitch{}, Manual: []ManualSwitch{}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	list, err := c.ListSwitches()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list.Standard) != 0 || len(list.Manual) != 0 {
		t.Errorf("expected empty lists, got %+v", list)
	}
}

// ---------------------------------------------------------------------------
// CreateStandardSwitch
// ---------------------------------------------------------------------------

func TestCreateStandardSwitch_Success(t *testing.T) {
	var gotName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/network/switch/standard" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %q", r.Method)
		}
		var req CreateStandardSwitchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotName = req.Name
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[interface{}]{Status: "ok"})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if err := c.CreateStandardSwitch("packer-switch"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotName != "packer-switch" {
		t.Errorf("switch name = %q, want %q", gotName, "packer-switch")
	}
}

func TestCreateStandardSwitch_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if err := c.CreateStandardSwitch("packer-switch"); err == nil {
		t.Fatal("expected error for 409 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetDHCPLeases error path
// ---------------------------------------------------------------------------

func TestGetDHCPLeases_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if _, err := c.GetDHCPLeases(); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListSwitches error path
// ---------------------------------------------------------------------------

func TestListSwitches_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if _, err := c.ListSwitches(); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// FindLeaseByMAC error path
// ---------------------------------------------------------------------------

func TestFindLeaseByMAC_GetDHCPLeases_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if _, err := c.FindLeaseByMAC("AA:BB:CC:DD:EE:FF"); err == nil {
		t.Fatal("expected error when DHCP lease API fails, got nil")
	}
}
