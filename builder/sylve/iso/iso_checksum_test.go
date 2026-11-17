// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package iso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

const testSHA = "bc85d747c6ee4354d14fbfeee38087500386a2e9d86fa7b5ab13d840d902aa33"

// checksumConfig is a valid sylve-iso configuration with iso_checksum.
func checksumConfig(checksum string) map[string]interface{} {
	return map[string]interface{}{
		"sylve_token":      "tok",
		"iso_download_url": "https://cdn.openbsd.org/pub/OpenBSD/8.0/amd64/install80.iso",
		"switch_name":      "sw",
		"ssh_username":     "root",
		"iso_checksum":     checksum,
	}
}

func TestConfig_ISOChecksum_AcceptsSHA256AnyCase(t *testing.T) {
	c, err := prepare(checksumConfig("SHA256:" + strings.ToUpper(testSHA)))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if c.ISOChecksum != "sha256:"+testSHA {
		t.Fatalf("ISOChecksum = %q, want it lower-cased", c.ISOChecksum)
	}
}

func TestConfig_ISOChecksum_RejectsOtherForms(t *testing.T) {
	for _, bad := range []string{"md5:0123456789abcdef0123456789abcdef", "sha256:" + testSHA[:63], testSHA, "file:https://x/SHA256"} {
		if _, err := prepare(checksumConfig(bad)); err == nil || !strings.Contains(err.Error(), "iso_checksum") {
			t.Errorf("prepare(iso_checksum %q) = %v, want an iso_checksum error", bad, err)
		}
	}
}

func TestDefaultISOSteps_ISOChecksumDownloadsChecksAndUploads(t *testing.T) {
	b := &Builder{config: Config{ISOChecksum: "sha256:" + testSHA, ISODownloadURL: "https://x/install80.iso"}}
	steps := b.defaultISOSteps()
	dl, ok := steps[0].(*commonsteps.StepDownload)
	if !ok || dl.Checksum != "sha256:"+testSHA || dl.ResultKey != "iso_path" || dl.Url[0] != "https://x/install80.iso" {
		t.Fatalf("steps[0] = %#v, want StepDownload of the ISO with its checksum", steps[0])
	}
	if _, ok := steps[1].(*StepUploadISO); !ok {
		t.Fatalf("steps[1] = %T, want *StepUploadISO", steps[1])
	}
	if _, ok := (&Builder{config: Config{}}).defaultISOSteps()[0].(*StepDownloadISO); !ok {
		t.Fatal("without iso_checksum steps[0] should stay StepDownloadISO")
	}
}

func TestVerifiedISOName(t *testing.T) {
	if got := verifiedISOName("sha256:"+testSHA, "https://cdn.openbsd.org/pub/OpenBSD/8.0/amd64/install80.iso"); got != "sha256-"+testSHA+"-install80.iso" {
		t.Fatalf("verifiedISOName = %q", got)
	}
}

// fakeUploadISOSylve is a Sylve API for StepUploadISO: statuses are the
// successive statuses of the upload's download (empty: not there yet); the
// last one repeats. listCode or uploadCode, when non-zero, fail that call;
// failListFrom, when non-zero, fails the list from that call on.
type fakeUploadISOSylve struct {
	statuses             []client.DownloadStatus
	listCode, uploadCode int
	failListFrom         int
	lists, uploads       int32
}

func (f *fakeUploadISOSylve) serve(t *testing.T) *httptest.Server {
	t.Helper()
	name := "sha256-" + testSHA + "-install80.iso"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads":
			n := int(atomic.AddInt32(&f.lists, 1))
			if f.listCode != 0 || (f.failListFrom != 0 && n >= f.failListFrom) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			data := []client.Download{{ID: 1, Name: name, Type: "http", UUID: "by-url", Status: client.DownloadStatusDone}}
			if st := f.statuses[min(n, len(f.statuses))-1]; st != "" {
				data = append(data, client.Download{ID: 2, Name: name, Type: client.DownloadTypePath, UUID: "uploaded", Status: st, Error: "disk full"})
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "success", Data: data})
		case r.URL.Path == "/api/utilities/downloader-uploads":
			atomic.AddInt32(&f.uploads, 1)
			if f.uploadCode != 0 {
				w.WriteHeader(f.uploadCode)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"status":"success","data":{"uploadId":"up-1"}}`))
		case r.URL.Path == "/api/utilities/downloader-uploads/up-1/complete":
			_, _ = w.Write([]byte(`{"status":"success","data":{"downloadId":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runUploadISO runs StepUploadISO against srv with millisecond polling.
func runUploadISO(t *testing.T, ctx context.Context, srv *httptest.Server) (multistep.StepAction, multistep.StateBag) {
	t.Helper()
	origPoll, origMax := uploadPollInterval, uploadMaxWait
	t.Cleanup(func() { uploadPollInterval, uploadMaxWait = origPoll, origMax })
	uploadPollInterval, uploadMaxWait = time.Millisecond, time.Second

	iso := filepath.Join(t.TempDir(), "install80.iso")
	if err := os.WriteFile(iso, []byte("iso"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("iso_path", iso)
	step := &StepUploadISO{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true,
		ISODownloadURL: "https://cdn.openbsd.org/pub/OpenBSD/8.0/amd64/install80.iso",
		ISOChecksum:    "sha256:" + testSHA,
	}}
	defer step.Cleanup(state)
	return step.Run(ctx, state), state
}

func TestStepUploadISO_ReusesTheVerifiedUpload(t *testing.T) {
	f := &fakeUploadISOSylve{statuses: []client.DownloadStatus{client.DownloadStatusDone}}
	got, state := runUploadISO(t, context.Background(), f.serve(t))
	if got != multistep.ActionContinue || state.Get("iso_uuid") != "uploaded" || f.uploads != 0 {
		t.Fatalf("Run() = %v, iso_uuid %v, uploads %d; want the existing upload, no new one", got, state.Get("iso_uuid"), f.uploads)
	}
}

func TestStepUploadISO_UploadsAndWaitsUntilDone(t *testing.T) {
	f := &fakeUploadISOSylve{statuses: []client.DownloadStatus{"", client.DownloadStatusProcessing, client.DownloadStatusDone}}
	got, state := runUploadISO(t, context.Background(), f.serve(t))
	if got != multistep.ActionContinue || state.Get("iso_uuid") != "uploaded" || f.uploads != 1 {
		t.Fatalf("Run() = %v (%v), iso_uuid %v, uploads %d; want one upload, its UUID", got, state.Get("error"), state.Get("iso_uuid"), f.uploads)
	}
}

func TestStepUploadISO_HaltsWhenSylveFailsTheUpload(t *testing.T) {
	f := &fakeUploadISOSylve{statuses: []client.DownloadStatus{"", client.DownloadStatusFailed}}
	got, state := runUploadISO(t, context.Background(), f.serve(t))
	if got != multistep.ActionHalt || !strings.Contains(state.Get("error").(error).Error(), "disk full") {
		t.Fatalf("Run() = %v (%v), want a halt naming Sylve's error", got, state.Get("error"))
	}
}

func TestStepUploadISO_HaltsOnErrors(t *testing.T) {
	for name, f := range map[string]*fakeUploadISOSylve{
		"list":    {listCode: http.StatusInternalServerError, statuses: []client.DownloadStatus{""}},
		"upload":  {uploadCode: http.StatusInsufficientStorage, statuses: []client.DownloadStatus{""}},
		"timeout": {statuses: []client.DownloadStatus{"", client.DownloadStatusProcessing}},
	} {
		t.Run(name, func(t *testing.T) {
			if got, state := runUploadISO(t, context.Background(), f.serve(t)); got != multistep.ActionHalt || state.Get("error") == nil {
				t.Fatalf("Run() = %v (%v), want a halt with an error", got, state.Get("error"))
			}
		})
	}
}

func TestStepUploadISO_HaltsWhenCancelled(t *testing.T) {
	f := &fakeUploadISOSylve{statuses: []client.DownloadStatus{client.DownloadStatusProcessing}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, state := runUploadISO(t, ctx, f.serve(t)); got != multistep.ActionHalt || state.Get("error") != context.Canceled {
		t.Fatalf("Run() = %v (%v), want a halt with %v", got, state.Get("error"), context.Canceled)
	}
}

func TestStepUploadISO_HaltsWhenListFailsWhileWaiting(t *testing.T) {
	f := &fakeUploadISOSylve{statuses: []client.DownloadStatus{""}, failListFrom: 2}
	if got, state := runUploadISO(t, context.Background(), f.serve(t)); got != multistep.ActionHalt || f.uploads != 1 {
		t.Fatalf("Run() = %v (%v), uploads %d; want a halt after the upload", got, state.Get("error"), f.uploads)
	}
}
