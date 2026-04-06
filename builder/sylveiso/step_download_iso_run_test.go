// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

const testISOURL = "https://example.com/packer-test.iso"

func testStepDownloadISOConfig(srvURL string) *Config {
	return &Config{
		SylveURL:       srvURL,
		SylveToken:     "tok",
		TLSSkipVerify:  true,
		ISODownloadURL: testISOURL,
	}
}

func TestStepDownloadISO_AlreadyDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/utilities/downloads" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{
			Status: "ok",
			Data: []client.Download{
				{URL: testISOURL, Status: client.DownloadStatusDone, UUID: "prior-uuid"},
			},
		})
	}))
	defer srv.Close()

	step := &StepDownloadISO{Config: testStepDownloadISOConfig(srv.URL)}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if state.Get("iso_uuid") != "prior-uuid" {
		t.Fatalf("iso_uuid = %v, want prior-uuid", state.Get("iso_uuid"))
	}
}

func TestStepDownloadISO_ExistingFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/utilities/downloads" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{
			Status: "ok",
			Data: []client.Download{
				{URL: testISOURL, Status: client.DownloadStatusFailed, Error: "disk full"},
			},
		})
	}))
	defer srv.Close()

	step := &StepDownloadISO{Config: testStepDownloadISOConfig(srv.URL)}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	err, ok := state.Get("error").(error)
	if !ok || err == nil {
		t.Fatal("expected error in state")
	}
}

func TestStepDownloadISO_TriggerDownloadFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{
				Status: "ok",
				Data:   []client.Download{},
			})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			http.Error(w, "bad", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepDownloadISO{Config: testStepDownloadISOConfig(srv.URL)}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	if _, ok := state.Get("error").(error); !ok {
		t.Fatal("expected error in state")
	}
}

func TestStepDownloadISO_ContextCancelledBeforePoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{
				Status: "ok",
				Data:   []client.Download{},
			})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	step := &StepDownloadISO{Config: testStepDownloadISOConfig(srv.URL)}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	if _, ok := state.Get("error").(error); !ok {
		t.Fatal("expected error in state")
	}
}

func TestStepDownloadISO_TriggerThenPollUntilDone(t *testing.T) {
	restoreDownloadISODurations(t)
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			var data []client.Download
			if n >= 2 {
				data = []client.Download{
					{URL: testISOURL, Status: client.DownloadStatusDone, UUID: "polled-uuid"},
				}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{
				Status: "ok",
				Data:   data,
			})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepDownloadISO{Config: testStepDownloadISOConfig(srv.URL)}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if state.Get("iso_uuid") != "polled-uuid" {
		t.Fatalf("iso_uuid = %v, want polled-uuid", state.Get("iso_uuid"))
	}
}

// TestStepDownloadISO_ProgressWhileProcessing exercises the default poll branch
// (processing + progress) and getRemoteFileSize via HEAD Content-Length.
func TestStepDownloadISO_ProgressWhileProcessing(t *testing.T) {
	restoreDownloadISODurations(t)
	isoHead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer isoHead.Close()
	isoURL := isoHead.URL + "/packer.iso"

	var getN int32
	sylve := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			var data []client.Download
			if n == 1 {
				data = []client.Download{
					{URL: isoURL, Status: client.DownloadStatusProcessing, Progress: 40, UUID: "u1"},
				}
			} else {
				data = []client.Download{
					{URL: isoURL, Status: client.DownloadStatusDone, UUID: "done-uuid"},
				}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: data})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sylve.Close()

	step := &StepDownloadISO{Config: testStepDownloadISOConfig(sylve.URL)}
	step.Config.ISODownloadURL = isoURL
	ui := newMockUI()
	state := new(multistep.BasicStateBag)
	state.Put("ui", ui)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if state.Get("iso_uuid") != "done-uuid" {
		t.Fatalf("iso_uuid = %v", state.Get("iso_uuid"))
	}
	if !ui.TrackProgressCalled {
		t.Fatal("expected progress tracker path (HEAD Content-Length > 0)")
	}
}

// TestStepDownloadISO_InitialListErrorThenTriggerDownload covers the branch
// where FindDownloadByURL fails (ListDownloads error): existing is cleared and
// TriggerDownload still runs.
func TestStepDownloadISO_InitialListErrorThenTriggerDownload(t *testing.T) {
	restoreDownloadISODurations(t)
	const isoURL = "https://example.com/after-list-err.iso"
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			if n == 1 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			data := []client.Download{{URL: isoURL, Status: client.DownloadStatusDone, UUID: "u1"}}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: data})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	step := &StepDownloadISO{Config: &Config{
		SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true, ISODownloadURL: isoURL,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if getN < 2 {
		t.Fatalf("expected ListDownloads retry after first error, getN=%d", getN)
	}
}

// TestStepDownloadISO_TrackerPollFailed covers failed download with an active
// isoProgressTracker (remote HEAD Content-Length > 0): tracker.cancel on fail.
func TestStepDownloadISO_TrackerPollFailed(t *testing.T) {
	restoreDownloadISODurations(t)
	headSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer headSrv.Close()
	u := headSrv.URL + "/tracker-fail.iso"

	var getN int32
	sylveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			var data []client.Download
			switch n {
			case 1:
				data = nil
			case 2:
				data = []client.Download{{URL: u, Status: client.DownloadStatusProcessing, UUID: "tid", Progress: 40}}
			default:
				data = []client.Download{{URL: u, Status: client.DownloadStatusFailed, Error: "checksum"}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: data})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sylveSrv.Close()

	step := &StepDownloadISO{Config: &Config{
		SylveURL: sylveSrv.URL, SylveToken: "tok", TLSSkipVerify: true, ISODownloadURL: u,
	}}
	state := new(multistep.BasicStateBag)
	ui := newMockUI()
	state.Put("ui", ui)

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	if !ui.TrackProgressCalled {
		t.Fatal("expected progress tracker for failed poll with remote size")
	}
}

// TestStepDownloadISO_TrackerContextCancelledDuringPoll covers ctx cancellation
// in the poll loop while a progress tracker is active.
func TestStepDownloadISO_TrackerContextCancelledDuringPoll(t *testing.T) {
	restoreDownloadISODurations(t)
	headSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer headSrv.Close()
	u := headSrv.URL + "/tracker-cancel.iso"

	ctx, cancel := context.WithCancel(context.Background())
	var cancelOnce sync.Once
	var getN int32
	sylveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			var data []client.Download
			switch n {
			case 1:
				data = nil
			case 2:
				data = []client.Download{{URL: u, Status: client.DownloadStatusProcessing, UUID: "u", Progress: 5}}
				cancelOnce.Do(func() {
					go func() {
						time.Sleep(2 * time.Millisecond)
						cancel()
					}()
				})
			default:
				data = []client.Download{{URL: u, Status: client.DownloadStatusProcessing, UUID: "u", Progress: 50}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: data})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sylveSrv.Close()

	step := &StepDownloadISO{Config: &Config{
		SylveURL: sylveSrv.URL, SylveToken: "tok", TLSSkipVerify: true, ISODownloadURL: u,
	}}
	state := new(multistep.BasicStateBag)
	ui := newMockUI()
	state.Put("ui", ui)

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	if ctx.Err() == nil {
		t.Fatal("expected cancelled context")
	}
	if !ui.TrackProgressCalled {
		t.Fatal("expected progress tracker path before cancel")
	}
}

// TestStepDownloadISO_TotalTimeoutWithTracker covers downloadISOTotalTimeout
// expiry while a progress tracker is active (tracker.cancel on timeout).
func TestStepDownloadISO_TotalTimeoutWithTracker(t *testing.T) {
	restoreDownloadISODurations(t)
	restoreDownloadISOTotalTimeout(t)

	headSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "5000")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer headSrv.Close()
	u := headSrv.URL + "/timeout-tracker.iso"

	var getN int32
	sylveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			var data []client.Download
			if n == 1 {
				data = nil
			} else {
				data = []client.Download{{URL: u, Status: client.DownloadStatusProcessing, UUID: "slow", Progress: 1}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: data})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sylveSrv.Close()

	step := &StepDownloadISO{Config: &Config{
		SylveURL: sylveSrv.URL, SylveToken: "tok", TLSSkipVerify: true, ISODownloadURL: u,
	}}
	state := new(multistep.BasicStateBag)
	ui := newMockUI()
	state.Put("ui", ui)

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	if !ui.TrackProgressCalled {
		t.Fatal("expected progress tracker before timeout")
	}
}

// TestStepDownloadISO_TotalTimeoutWithoutTracker covers timeout when no remote
// Content-Length was available (no progress tracker).
func TestStepDownloadISO_TotalTimeoutWithoutTracker(t *testing.T) {
	restoreDownloadISODurations(t)
	restoreDownloadISOTotalTimeout(t)

	const isoURL = "https://example.com/timeout-no-head.iso"
	var getN int32
	sylveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodGet:
			n := atomic.AddInt32(&getN, 1)
			var data []client.Download
			if n == 1 {
				data = nil
			} else {
				data = []client.Download{{URL: isoURL, Status: client.DownloadStatusProcessing, UUID: "x", Progress: 1}}
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[[]client.Download]{Status: "ok", Data: data})
		case r.URL.Path == "/api/utilities/downloads" && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(client.APIResponse[interface{}]{Status: "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer sylveSrv.Close()

	step := &StepDownloadISO{Config: &Config{
		SylveURL: sylveSrv.URL, SylveToken: "tok", TLSSkipVerify: true, ISODownloadURL: isoURL,
	}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}
