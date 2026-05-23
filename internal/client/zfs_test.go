// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveZFS starts a handler that responds to multiple path/method pairs. Paths
// not present in the map get a 404.
func serveZFS(t *testing.T, routes map[string]http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.RequestURI()
		if h, ok := routes[key]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	c := New(srv.URL, "tok", false)
	return c, srv
}

// ---------------------------------------------------------------------------
// TakeDatasetSnapshot
// ---------------------------------------------------------------------------

func TestTakeDatasetSnapshot_Success(t *testing.T) {
	snapshotList := APIResponse[[]datasetInfo]{
		Status: "ok",
		Data: []datasetInfo{
			{GUID: "guid-snap-1", Name: "zroot/vms/myvm@packer-snap"},
		},
	}
	routes := map[string]http.HandlerFunc{
		"POST /api/zfs/datasets/snapshot": func(w http.ResponseWriter, r *http.Request) {
			var req createSnapshotRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			if req.GUID != "ds-guid" || req.Name != "packer-snap" {
				http.Error(w, "unexpected body", http.StatusBadRequest)
				return
			}
			okJSON(w, APIResponse[interface{}]{Status: "ok"})
		},
		"GET /api/zfs/datasets?type=SNAPSHOT": func(w http.ResponseWriter, r *http.Request) {
			okJSON(w, snapshotList)
		},
	}
	c, srv := serveZFS(t, routes)
	defer srv.Close()

	guid, err := c.TakeDatasetSnapshot("ds-guid", "zroot/vms/myvm", "packer-snap")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if guid != "guid-snap-1" {
		t.Errorf("snapshot GUID = %q, want %q", guid, "guid-snap-1")
	}
}

func TestTakeDatasetSnapshot_PostError(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"POST /api/zfs/datasets/snapshot": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		},
	}
	c, srv := serveZFS(t, routes)
	defer srv.Close()

	if _, err := c.TakeDatasetSnapshot("guid", "zroot/vms/x", "snap"); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestTakeDatasetSnapshot_NotFoundAfterCreate(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"POST /api/zfs/datasets/snapshot": func(w http.ResponseWriter, r *http.Request) {
			okJSON(w, APIResponse[interface{}]{Status: "ok"})
		},
		"GET /api/zfs/datasets?type=SNAPSHOT": func(w http.ResponseWriter, r *http.Request) {
			okJSON(w, APIResponse[[]datasetInfo]{Data: []datasetInfo{}})
		},
	}
	c, srv := serveZFS(t, routes)
	defer srv.Close()

	if _, err := c.TakeDatasetSnapshot("guid", "zroot/vms/x", "snap"); err == nil {
		t.Fatal("expected error when snapshot is not in list, got nil")
	}
}

func TestTakeDatasetSnapshot_ListSnapshotsRequestFails(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"POST /api/zfs/datasets/snapshot": func(w http.ResponseWriter, r *http.Request) {
			okJSON(w, APIResponse[interface{}]{Status: "ok"})
		},
		"GET /api/zfs/datasets?type=SNAPSHOT": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"status":"error"}`, http.StatusBadGateway)
		},
	}
	c, srv := serveZFS(t, routes)
	defer srv.Close()

	if _, err := c.TakeDatasetSnapshot("guid", "zpool/ds", "snap"); err == nil {
		t.Fatal("expected error when listing snapshots fails after create")
	}
}

// ---------------------------------------------------------------------------
// RollbackDataset
// ---------------------------------------------------------------------------

func TestRollbackDataset_Success(t *testing.T) {
	var gotReq rollbackSnapshotRequest
	routes := map[string]http.HandlerFunc{
		"POST /api/zfs/datasets/snapshot/rollback": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotReq)
			okJSON(w, APIResponse[interface{}]{Status: "ok"})
		},
	}
	c, srv := serveZFS(t, routes)
	defer srv.Close()

	if err := c.RollbackDataset("snap-guid", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.GUID != "snap-guid" || !gotReq.DestroyMoreRecent {
		t.Errorf("unexpected request: %+v", gotReq)
	}
}

func TestRollbackDataset_Error(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"POST /api/zfs/datasets/snapshot/rollback": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "conflict", http.StatusConflict)
		},
	}
	c, srv := serveZFS(t, routes)
	defer srv.Close()

	if err := c.RollbackDataset("guid", false); err == nil {
		t.Fatal("expected error for 409 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// listDatasets (private, tested indirectly via TakeDatasetSnapshot)
// Direct test to verify JSON deserialization shape.
// ---------------------------------------------------------------------------

func TestListDatasets_DirectArray(t *testing.T) {
	// Verify that the response is decoded as a direct array under "data",
	// not double-nested (i.e. data.data).
	datasets := []datasetInfo{
		{GUID: "g1", Name: "zroot/vms/x@snap1"},
		{GUID: "g2", Name: "zroot/vms/y@snap2"},
	}
	routes := map[string]http.HandlerFunc{
		"GET /api/zfs/datasets?type=SNAPSHOT": func(w http.ResponseWriter, r *http.Request) {
			okJSON(w, APIResponse[[]datasetInfo]{Status: "ok", Data: datasets})
		},
	}
	c, srv := serveZFS(t, routes)
	defer srv.Close()

	got, err := c.listDatasets("SNAPSHOT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 datasets, got %d", len(got))
	}
	if got[0].GUID != "g1" || got[1].Name != "zroot/vms/y@snap2" {
		t.Errorf("unexpected datasets: %+v", got)
	}
}

func TestListDatasets_RequestError(t *testing.T) {
	routes := map[string]http.HandlerFunc{
		"GET /api/zfs/datasets?type=VOLUME": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusBadGateway)
		},
	}
	c, srv := serveZFS(t, routes)
	defer srv.Close()

	if _, err := c.listDatasets("VOLUME"); err == nil {
		t.Fatal("expected error when GET datasets fails")
	}
}
