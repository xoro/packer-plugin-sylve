// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// fakeUploadSylve answers the downloader upload routes. uploadCode or
// completeCode, when non-zero, replaces the success answer; noUploadID leaves
// the receipt's ID empty. It records what arrived.
type fakeUploadSylve struct {
	uploadCode, completeCode int
	noUploadID               bool
	gotField, gotName        string
	gotContent               []byte
	gotType                  string
	aborted                  int32
}

func (f *fakeUploadSylve) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloader-uploads" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			reader, err := r.MultipartReader()
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			part, err := reader.NextPart()
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.gotField, f.gotName = part.FormName(), part.FileName()
			f.gotContent, _ = io.ReadAll(part)
			if f.uploadCode != 0 {
				w.WriteHeader(f.uploadCode)
				return
			}
			id := "up-1"
			if f.noUploadID {
				id = ""
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(APIResponse[downloaderUploadReceipt]{Status: "success", Data: downloaderUploadReceipt{UploadID: id}})
		case r.URL.Path == "/api/utilities/downloader-uploads/up-1/complete" && r.Method == http.MethodPost:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.gotType = body["downloadType"]
			if f.completeCode != 0 {
				w.WriteHeader(f.completeCode)
				return
			}
			_ = json.NewEncoder(w).Encode(APIResponse[downloaderUploadCompletion]{Status: "success", Data: downloaderUploadCompletion{DownloadID: 9}})
		case r.URL.Path == "/api/utilities/downloader-uploads/up-1" && r.Method == http.MethodDelete:
			atomic.AddInt32(&f.aborted, 1)
			_ = json.NewEncoder(w).Encode(APIResponse[interface{}]{Status: "success"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// isoFile writes a small stand-in ISO and returns its path.
func isoFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "install80.iso")
	if err := os.WriteFile(path, []byte("verified iso bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUploadFile_StreamsAndCompletes(t *testing.T) {
	f := &fakeUploadSylve{}
	srv := f.serve(t)

	id, err := newTestClient(srv.URL, "tok").UploadFile(context.Background(), isoFile(t), "sha256-ab-install80.iso")
	if err != nil || id != 9 {
		t.Fatalf("UploadFile = %d, %v; want 9, nil", id, err)
	}
	if f.gotField != "filepond" || f.gotName != "sha256-ab-install80.iso" || string(f.gotContent) != "verified iso bytes" {
		t.Fatalf("upload arrived as field %q name %q content %q", f.gotField, f.gotName, f.gotContent)
	}
	if f.gotType != "uncategorized" {
		t.Fatalf("complete downloadType = %q, want uncategorized", f.gotType)
	}
}

func TestUploadFile_MissingFile(t *testing.T) {
	srv := (&fakeUploadSylve{}).serve(t)
	if _, err := newTestClient(srv.URL, "tok").UploadFile(context.Background(), filepath.Join(t.TempDir(), "none.iso"), "x.iso"); err == nil {
		t.Fatal("UploadFile: want error for a missing file, got nil")
	}
}

func TestUploadFile_UploadRejected(t *testing.T) {
	srv := (&fakeUploadSylve{uploadCode: http.StatusInsufficientStorage}).serve(t)
	if _, err := newTestClient(srv.URL, "tok").UploadFile(context.Background(), isoFile(t), "x.iso"); err == nil {
		t.Fatal("UploadFile: want error for HTTP 507, got nil")
	}
}

func TestUploadFile_NoUploadID(t *testing.T) {
	srv := (&fakeUploadSylve{noUploadID: true}).serve(t)
	if _, err := newTestClient(srv.URL, "tok").UploadFile(context.Background(), isoFile(t), "x.iso"); err == nil {
		t.Fatal("UploadFile: want error for a receipt without ID, got nil")
	}
}

func TestUploadFile_FailedCompletionAborts(t *testing.T) {
	f := &fakeUploadSylve{completeCode: http.StatusConflict}
	srv := f.serve(t)
	if _, err := newTestClient(srv.URL, "tok").UploadFile(context.Background(), isoFile(t), "x.iso"); err == nil {
		t.Fatal("UploadFile: want error for a failed completion, got nil")
	}
	if atomic.LoadInt32(&f.aborted) != 1 {
		t.Fatalf("staged upload aborted %d times, want 1", f.aborted)
	}
}

func TestUploadFile_Unreachable(t *testing.T) {
	srv := (&fakeUploadSylve{}).serve(t)
	srv.Close()
	if _, err := newTestClient(srv.URL, "tok").UploadFile(context.Background(), isoFile(t), "x.iso"); err == nil {
		t.Fatal("UploadFile: want error for an unreachable Sylve, got nil")
	}
}

func TestUploadFile_BadURL(t *testing.T) {
	if _, err := newTestClient("http://bad host", "tok").UploadFile(context.Background(), isoFile(t), "x.iso"); err == nil {
		t.Fatal("UploadFile: want error for an invalid base URL, got nil")
	}
}

// downloadsServer answers GET /api/utilities/downloads with downloads, or with
// HTTP 500 when downloads is nil.
func downloadsServer(t *testing.T, downloads []Download) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if downloads == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(APIResponse[[]Download]{Status: "success", Data: downloads})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFindUploadedDownload_OnlyAnUploadOfThatName(t *testing.T) {
	srv := downloadsServer(t, []Download{
		{ID: 1, Name: "sha256-ab-install80.iso", Type: "http", UUID: "by-url"},
		{ID: 2, Name: "other.iso", Type: DownloadTypePath, UUID: "other"},
		{ID: 3, Name: "sha256-ab-install80.iso", Type: DownloadTypePath, UUID: "uploaded"},
	})
	dl, err := newTestClient(srv.URL, "tok").FindUploadedDownload("sha256-ab-install80.iso")
	if err != nil || dl == nil || dl.UUID != "uploaded" {
		t.Fatalf("FindUploadedDownload = %+v, %v; want the upload (uuid uploaded)", dl, err)
	}
}

func TestFindUploadedDownload_None(t *testing.T) {
	srv := downloadsServer(t, []Download{{ID: 1, Name: "sha256-ab-install80.iso", Type: "http"}})
	dl, err := newTestClient(srv.URL, "tok").FindUploadedDownload("sha256-ab-install80.iso")
	if err != nil || dl != nil {
		t.Fatalf("FindUploadedDownload = %+v, %v; want nil, nil (a URL download never matches)", dl, err)
	}
}

func TestFindUploadedDownload_Error(t *testing.T) {
	srv := downloadsServer(t, nil)
	if _, err := newTestClient(srv.URL, "tok").FindUploadedDownload("x.iso"); err == nil {
		t.Fatal("FindUploadedDownload: want error for HTTP 500, got nil")
	}
}

func TestUploadFile_ResponseCutOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"st`))
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	if _, err := newTestClient(srv.URL, "tok").UploadFile(context.Background(), isoFile(t), "x.iso"); err == nil {
		t.Fatal("UploadFile: want error for a response cut off, got nil")
	}
}
