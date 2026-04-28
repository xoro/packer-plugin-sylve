// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

func TestEnsureAuth_Login401DoesNotSpin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := &Builder{config: Config{
		SylveURL:      srv.URL,
		SylveUser:     "alice",
		SylvePassword: "wrong",
		SylveAuthType: "sylve",
		TLSSkipVerify: true,
	}}
	b.config.sylveAPILoginTimeoutDur = 10 * time.Minute

	start := time.Now()
	_, err := b.ensureAuth(newMockUI())
	if err == nil {
		t.Fatal("expected error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("401 login should fail immediately, took %v", time.Since(start))
	}
}

func TestEnsureAuth_PreexistingToken(t *testing.T) {
	b := &Builder{config: Config{SylveToken: "tok"}}
	cleanup, err := b.ensureAuth(newMockUI())
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup != nil {
		t.Fatal("expected nil cleanup when token already set")
	}
}

// TestEnsureAuth_UsesDefaultWaitBudgetWhenConfiguredZero covers ensureAuth when
// sylveAPILoginTimeoutDur is zero: the login loop should use the 2-minute default.
func TestEnsureAuth_UsesDefaultWaitBudgetWhenConfiguredZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "session-token"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := &Builder{config: Config{
		SylveURL:      srv.URL,
		SylveUser:     "alice",
		SylvePassword: "secret",
		SylveAuthType: "sylve",
		TLSSkipVerify: true,
	}}
	b.config.sylveAPILoginTimeoutDur = 0

	cleanup, err := b.ensureAuth(newMockUI())
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup func")
	}
	cleanup()
}

func TestEnsureAuth_LoginAndLogout(t *testing.T) {
	var logins, logouts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost:
			atomic.AddInt32(&logins, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "session-token"},
			})
		case r.URL.Path == "/api/auth/logout" && r.Method == http.MethodPost:
			atomic.AddInt32(&logouts, 1)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := &Builder{config: Config{
		SylveURL:      srv.URL,
		SylveUser:     "alice",
		SylvePassword: "secret",
		SylveAuthType: "sylve",
		TLSSkipVerify: true,
	}}
	ui := newMockUI()
	cleanup, err := b.ensureAuth(ui)
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup func")
	}
	if b.config.SylveToken != "session-token" {
		t.Fatalf("token = %q", b.config.SylveToken)
	}
	cleanup()
	if logins != 1 || logouts != 1 {
		t.Fatalf("logins=%d logouts=%d", logins, logouts)
	}
}

func TestEnsureAuth_LogoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "session-token"},
			})
		case r.URL.Path == "/api/auth/logout" && r.Method == http.MethodPost:
			http.Error(w, "server", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := &Builder{config: Config{
		SylveURL:      srv.URL,
		SylveUser:     "alice",
		SylvePassword: "secret",
		SylveAuthType: "sylve",
		TLSSkipVerify: true,
	}}
	ui := newMockUI()
	cleanup, err := b.ensureAuth(ui)
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup")
	}
	cleanup()
	if !ui.ErrorCalled {
		t.Fatal("expected ui.Error on logout failure")
	}
}

// TestEnsureAuth_SucceedsAfter503Burst verifies the outer login loop: the first
// Login exhausts HTTP retries on 503; the second attempt succeeds. Uses a zero
// sylveLoginRetryInterval so the sleep path uses the sub-millisecond fallback.
func TestEnsureAuth_SucceedsAfter503Burst(t *testing.T) {
	orig := sylveLoginRetryInterval
	sylveLoginRetryInterval = 0
	t.Cleanup(func() { sylveLoginRetryInterval = orig })

	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			c := atomic.AddInt32(&n, 1)
			if c <= 5 {
				http.Error(w, "bad", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "ok-token"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := &Builder{config: Config{
		SylveURL:      srv.URL,
		SylveUser:     "alice",
		SylvePassword: "secret",
		SylveAuthType: "sylve",
		TLSSkipVerify: true,
	}}
	b.config.sylveAPILoginTimeoutDur = 10 * time.Minute

	cleanup, err := b.ensureAuth(newMockUI())
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup")
	}
	if b.config.SylveToken != "ok-token" {
		t.Fatalf("token = %q", b.config.SylveToken)
	}
	cleanup()
	if n != 6 {
		t.Fatalf("login HTTP requests = %d, want 6", n)
	}
}

// TestEnsureAuth_TimesOutWaitingForAPI covers the deadline branch after a full
// Login fails with retriable errors (inner HTTP retries add a few seconds).
func TestEnsureAuth_TimesOutWaitingForAPI(t *testing.T) {
	orig := sylveLoginRetryInterval
	sylveLoginRetryInterval = time.Millisecond
	t.Cleanup(func() { sylveLoginRetryInterval = orig })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			http.Error(w, "bad", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := &Builder{config: Config{
		SylveURL:      srv.URL,
		SylveUser:     "alice",
		SylvePassword: "secret",
		SylveAuthType: "sylve",
		TLSSkipVerify: true,
	}}
	b.config.sylveAPILoginTimeoutDur = 50 * time.Millisecond

	_, err := b.ensureAuth(newMockUI())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v", err)
	}
}

// TestEnsureAuth_TruncatesRetrySleepToDeadline exercises ensureAuth when the
// retry interval is larger than the time remaining until the login deadline.
func TestEnsureAuth_TruncatesRetrySleepToDeadline(t *testing.T) {
	orig := sylveLoginRetryInterval
	sylveLoginRetryInterval = time.Minute
	t.Cleanup(func() { sylveLoginRetryInterval = orig })

	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			c := atomic.AddInt32(&n, 1)
			if c == 1 {
				http.Error(w, "bad", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "retry-token"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	b := &Builder{config: Config{
		SylveURL:      srv.URL,
		SylveUser:     "alice",
		SylvePassword: "secret",
		SylveAuthType: "sylve",
		TLSSkipVerify: true,
	}}
	b.config.sylveAPILoginTimeoutDur = 1500 * time.Millisecond

	start := time.Now()
	cleanup, err := b.ensureAuth(newMockUI())
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	cleanup()
	if n != 2 {
		t.Fatalf("login attempts = %d, want 2", n)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("expected truncated sleep (deadline ~1.5s), took %v", time.Since(start))
	}
}
