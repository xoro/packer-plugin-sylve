// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// serveDownloads starts an httptest server that returns the given downloads
// from GET /api/utilities/downloads and returns a Client pointed at it.
func serveDownloads(t *testing.T, downloads []Download) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/utilities/downloads" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[[]Download]{
			Status: "ok",
			Data:   downloads,
		})
	}))
	c := New(srv.URL, "tok", true)
	return c, srv
}

// ---------------------------------------------------------------------------
// FindDownloadByURL
// ---------------------------------------------------------------------------

func TestFindDownloadByURL_Found(t *testing.T) {
	const iso = "https://example.com/os.iso"
	c, srv := serveDownloads(t, []Download{
		{ID: 1, URL: "https://example.com/other.iso", Status: DownloadStatusDone},
		{ID: 2, URL: iso, Status: DownloadStatusDone, UUID: "abc-123"},
	})
	defer srv.Close()

	d, err := c.FindDownloadByURL(iso)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("FindDownloadByURL: expected a match, got nil")
	}
	if d.UUID != "abc-123" {
		t.Errorf("UUID = %q, want %q", d.UUID, "abc-123")
	}
}

func TestFindDownloadByURL_NotFound(t *testing.T) {
	c, srv := serveDownloads(t, []Download{
		{ID: 1, URL: "https://example.com/other.iso"},
	})
	defer srv.Close()

	d, err := c.FindDownloadByURL("https://example.com/missing.iso")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil for unknown URL, got %+v", d)
	}
}

func TestFindDownloadByURL_EmptyList(t *testing.T) {
	c, srv := serveDownloads(t, []Download{})
	defer srv.Close()

	d, err := c.FindDownloadByURL("https://example.com/os.iso")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil for empty list, got %+v", d)
	}
}

func TestFindDownloadByURL_URLMustMatchExactly(t *testing.T) {
	c, srv := serveDownloads(t, []Download{
		{ID: 1, URL: "https://example.com/os.iso"},
	})
	defer srv.Close()

	// A URL that is a prefix of the stored URL must not match.
	d, err := c.FindDownloadByURL("https://example.com/os")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Errorf("partial URL should not match, got %+v", d)
	}
}

func TestFindDownloadByURL_ReturnsFirstMatch(t *testing.T) {
	// If two entries share the same URL, the first should be returned.
	const iso = "https://example.com/os.iso"
	c, srv := serveDownloads(t, []Download{
		{ID: 1, URL: iso, UUID: "first"},
		{ID: 2, URL: iso, UUID: "second"},
	})
	defer srv.Close()

	d, err := c.FindDownloadByURL(iso)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected a match, got nil")
	}
	if d.UUID != "first" {
		t.Errorf("UUID = %q, want %q (first entry)", d.UUID, "first")
	}
}

// ---------------------------------------------------------------------------
// DownloadStatus constants
// ---------------------------------------------------------------------------

func TestDownloadStatus_Constants(t *testing.T) {
	if DownloadStatusPending != "pending" {
		t.Errorf("DownloadStatusPending = %q, want %q", DownloadStatusPending, "pending")
	}
	if DownloadStatusProcessing != "processing" {
		t.Errorf("DownloadStatusProcessing = %q, want %q", DownloadStatusProcessing, "processing")
	}
	if DownloadStatusDone != "done" {
		t.Errorf("DownloadStatusDone = %q, want %q", DownloadStatusDone, "done")
	}
	if DownloadStatusFailed != "failed" {
		t.Errorf("DownloadStatusFailed = %q, want %q", DownloadStatusFailed, "failed")
	}
}

// ---------------------------------------------------------------------------
// TriggerDownload
// ---------------------------------------------------------------------------

func TestTriggerDownload_Success(t *testing.T) {
	var gotReq DownloadFileRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/utilities/downloads" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[interface{}]{Status: "ok"})
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if err := c.TriggerDownload("https://example.com/os.iso"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.URL != "https://example.com/os.iso" {
		t.Errorf("request URL = %q, want %q", gotReq.URL, "https://example.com/os.iso")
	}
	if gotReq.Type != "http" || gotReq.UType != "Packer" {
		t.Errorf("unexpected type/utype: %q/%q", gotReq.Type, gotReq.UType)
	}
}

func TestTriggerDownload_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if err := c.TriggerDownload("https://example.com/os.iso"); err == nil {
		t.Fatal("expected error for 400 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListDownloads
// ---------------------------------------------------------------------------

func TestListDownloads_Success(t *testing.T) {
	c, srv := serveDownloads(t, []Download{
		{ID: 1, UUID: "abc", URL: "https://example.com/os.iso", Status: DownloadStatusDone},
		{ID: 2, UUID: "def", URL: "https://other.com/other.iso", Status: DownloadStatusPending},
	})
	defer srv.Close()

	downloads, err := c.ListDownloads()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(downloads) != 2 {
		t.Errorf("expected 2 downloads, got %d", len(downloads))
	}
	if downloads[0].UUID != "abc" {
		t.Errorf("first download UUID = %q, want %q", downloads[0].UUID, "abc")
	}
}

func TestListDownloads_Empty(t *testing.T) {
	c, srv := serveDownloads(t, []Download{})
	defer srv.Close()

	downloads, err := c.ListDownloads()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(downloads) != 0 {
		t.Errorf("expected 0 downloads, got %d", len(downloads))
	}
}

// ---------------------------------------------------------------------------
// ListDownloads error path
// ---------------------------------------------------------------------------

func TestListDownloads_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if _, err := c.ListDownloads(); err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// ---------------------------------------------------------------------------
// FindDownloadByURL error path (when ListDownloads fails)
// ---------------------------------------------------------------------------

func TestFindDownloadByURL_ListError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok", false)
	if _, err := c.FindDownloadByURL("https://example.com/os.iso"); err == nil {
		t.Fatal("expected error when ListDownloads fails, got nil")
	}
}
