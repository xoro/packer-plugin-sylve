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

func TestGetRemoteFileSize_ContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	size := getRemoteFileSize(ctx, "https://example.com/file.iso")
	if size != 0 {
		t.Errorf("cancelled context: size = %d, want 0", size)
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

func TestIsoProgressTracker_AdvanceWritesAndDoneCloses(t *testing.T) {
	pr, pw := io.Pipe()
	readDone := make(chan struct{})
	var data []byte
	var readErr error
	go func() {
		data, readErr = io.ReadAll(pr)
		_ = pr.Close()
		close(readDone)
	}()

	tracker := &isoProgressTracker{pw: pw, fakeTotalSz: 1000, lastPct: 0}
	tracker.advance(50)
	tracker.done()

	<-readDone
	if readErr != nil {
		t.Fatalf("read fake progress bytes: %v", readErr)
	}
	// advance(50) writes 500; done() advances to 100% and writes another 500.
	if len(data) != 1000 {
		t.Fatalf("read %d bytes, want 1000", len(data))
	}
}

func TestIsoProgressTracker_CancelClosesWithError(t *testing.T) {
	pr, pw := io.Pipe()
	tracker := &isoProgressTracker{pw: pw, fakeTotalSz: 100, lastPct: 0}
	tracker.cancel()
	_, err := io.ReadAll(pr)
	if err == nil {
		t.Fatal("expected error from cancel/CloseWithError")
	}
}

func TestIsoProgressTracker_AdvanceSkipsWhenIntegerDeltaZero(t *testing.T) {
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
	// Integer division: 1% of 1 byte is 0 — advance should not bump lastPct.
	tracker := &isoProgressTracker{pw: pw, fakeTotalSz: 1, lastPct: 0}
	tracker.advance(1)
	if tracker.lastPct != 0 {
		t.Fatalf("lastPct = %d, want 0 (no write when delta==0)", tracker.lastPct)
	}
}
