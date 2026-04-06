// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// getRemoteFileSize
// ---------------------------------------------------------------------------

func TestGetRemoteFileSize_WithContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	size := getRemoteFileSize(context.Background(), srv.URL+"/file.iso")
	if size != 1048576 {
		t.Errorf("getRemoteFileSize = %d, want 1048576", size)
	}
}

func TestGetRemoteFileSize_NoContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	size := getRemoteFileSize(context.Background(), srv.URL+"/file.iso")
	if size != 0 {
		t.Errorf("getRemoteFileSize without Content-Length = %d, want 0", size)
	}
}

func TestGetRemoteFileSize_NegativeContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Explicitly unset Content-Length by hijacking the header so the
		// server sends a chunked response with no Content-Length header.
		w.Header().Del("Content-Length")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// A response with ContentLength=-1 (unknown) must return 0.
	size := getRemoteFileSize(context.Background(), srv.URL+"/file.iso")
	if size != 0 {
		t.Errorf("getRemoteFileSize without Content-Length = %d, want 0", size)
	}
}

func TestGetRemoteFileSize_ConnectionRefused(t *testing.T) {
	// Point at a port where nothing is listening.
	size := getRemoteFileSize(context.Background(), "http://127.0.0.1:19998/file.iso")
	if size != 0 {
		t.Errorf("getRemoteFileSize for refused connection = %d, want 0", size)
	}
}

func TestGetRemoteFileSize_MalformedURL(t *testing.T) {
	size := getRemoteFileSize(context.Background(), "http://a b/file.iso")
	if size != 0 {
		t.Errorf("getRemoteFileSize for malformed URL = %d, want 0", size)
	}
}

func TestIsoProgressTracker_AdvanceNoOpWhenPctDoesNotIncrease(t *testing.T) {
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	tracker := &isoProgressTracker{pw: pw, fakeTotalSz: 100, lastPct: 50}
	tracker.advance(50)
	tracker.advance(40)
}
