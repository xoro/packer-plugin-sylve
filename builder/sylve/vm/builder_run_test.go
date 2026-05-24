// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packer "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// stubArtifactStep populates state so buildArtifact succeeds without a live API.
type stubArtifactStep struct{}

func (stubArtifactStep) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	state.Put("vm_id", uint(10))
	state.Put("vm_rid", uint(20))
	return multistep.ActionContinue
}

func (stubArtifactStep) Cleanup(multistep.StateBag) {}

// haltErrorStep puts a fixed error in the state bag so Builder.Run surfaces it.
type haltErrorStep struct{ err error }

func (h haltErrorStep) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	state.Put("error", h.err)
	return multistep.ActionHalt
}

func (haltErrorStep) Cleanup(multistep.StateBag) {}

func TestBuilder_Run_Success(t *testing.T) {
	t.Cleanup(func() { vmBuildStepsHook = nil })
	vmBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{stubArtifactStep{}}
	}

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"vm_name":      "my-vm",
		"sylve_token":  "tok",
		"communicator": "none",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	art, err := b.Run(context.Background(), packer.TestUi(t), &packer.MockHook{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if art == nil {
		t.Fatal("Run returned nil artifact")
	}
}

func TestBuilder_Run_HaltedByStep(t *testing.T) {
	t.Cleanup(func() { vmBuildStepsHook = nil })
	sentinel := errors.New("step failed")
	vmBuildStepsHook = func(_ *Builder) []multistep.Step {
		return []multistep.Step{haltErrorStep{err: sentinel}}
	}

	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{
		"vm_name":      "my-vm",
		"sylve_token":  "tok",
		"communicator": "none",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	_, runErr := b.Run(context.Background(), packer.TestUi(t), &packer.MockHook{})
	if runErr == nil {
		t.Fatal("expected error from halted step")
	}
	if !errors.Is(runErr, sentinel) {
		t.Errorf("error = %v, want sentinel", runErr)
	}
}

func TestEnsureAuth_PreexistingToken(t *testing.T) {
	b := &Builder{config: Config{SylveToken: "tok"}}
	cleanup, err := b.ensureAuth(packer.TestUi(t))
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup != nil {
		t.Fatal("expected nil cleanup when token already set")
	}
}

func TestEnsureAuth_Login_Success(t *testing.T) {
	origRetry := sylveLoginRetryInterval
	sylveLoginRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() { sylveLoginRetryInterval = origRetry })

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
	t.Cleanup(srv.Close)

	b := &Builder{config: Config{
		SylveURL:                srv.URL,
		SylveUser:               "alice",
		SylvePassword:           "secret",
		SylveAuthType:           "sylve",
		TLSSkipVerify:           true,
		sylveAPILoginTimeoutDur: 30 * time.Second,
	}}

	cleanup, err := b.ensureAuth(packer.TestUi(t))
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup != nil {
		cleanup()
	}
}

func TestEnsureAuth_Login_401_Fails_Immediately(t *testing.T) {
	origRetry := sylveLoginRetryInterval
	sylveLoginRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() { sylveLoginRetryInterval = origRetry })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	b := &Builder{config: Config{
		SylveURL:                srv.URL,
		SylveUser:               "alice",
		SylvePassword:           "wrong",
		SylveAuthType:           "sylve",
		TLSSkipVerify:           true,
		sylveAPILoginTimeoutDur: 10 * time.Minute,
	}}

	start := time.Now()
	_, err := b.ensureAuth(packer.TestUi(t))
	if err == nil {
		t.Fatal("expected error")
	}
	// 401 should not spin — it must fail fast.
	if time.Since(start) > 2*time.Second {
		t.Fatalf("401 login should fail immediately, took %v", time.Since(start))
	}
}

func TestEnsureAuth_Retry_ThenSuccess(t *testing.T) {
	origRetry := sylveLoginRetryInterval
	sylveLoginRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() { sylveLoginRetryInterval = origRetry })

	// Return a retriable error on first attempt, succeed on second.
	var attempt int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost {
			attempt++
			if attempt == 1 {
				// 503 is treated as retriable by IsRetriableLoginWaitError.
				http.Error(w, `{"status":"error","message":"service unavailable"}`, http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "session-token"},
			})
			return
		}
		if r.URL.Path == "/api/auth/logout" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	b := &Builder{config: Config{
		SylveURL:                srv.URL,
		SylveUser:               "alice",
		SylvePassword:           "secret",
		SylveAuthType:           "sylve",
		TLSSkipVerify:           true,
		sylveAPILoginTimeoutDur: 5 * time.Second,
	}}

	cleanup, err := b.ensureAuth(packer.TestUi(t))
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup != nil {
		cleanup()
	}
}

func TestEnsureAuth_Retry_Timeout(t *testing.T) {
	origRetry := sylveLoginRetryInterval
	sylveLoginRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() { sylveLoginRetryInterval = origRetry })

	// Always return 503 (retriable) so the deadline is exhausted.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"status":"error","message":"service unavailable"}`, http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	b := &Builder{config: Config{
		SylveURL:                srv.URL,
		SylveUser:               "alice",
		SylvePassword:           "secret",
		SylveAuthType:           "sylve",
		TLSSkipVerify:           true,
		sylveAPILoginTimeoutDur: 30 * time.Millisecond,
	}}

	_, err := b.ensureAuth(packer.TestUi(t))
	if err == nil {
		t.Fatal("expected error when login deadline exhausted")
	}
}

func TestEnsureAuth_Logout_SuccessMessage(t *testing.T) {
	origRetry := sylveLoginRetryInterval
	sylveLoginRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() { sylveLoginRetryInterval = origRetry })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/auth/login" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.LoginResponse]{
				Status: "ok",
				Data:   client.LoginResponse{Token: "session-token"},
			})
		case r.URL.Path == "/api/auth/logout" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusOK)
			return
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	b := &Builder{config: Config{
		SylveURL:                srv.URL,
		SylveUser:               "alice",
		SylvePassword:           "secret",
		SylveAuthType:           "sylve",
		TLSSkipVerify:           true,
		sylveAPILoginTimeoutDur: 30 * time.Second,
	}}

	cleanup, err := b.ensureAuth(packer.TestUi(t))
	if err != nil {
		t.Fatalf("ensureAuth: %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected logout cleanup callback")
	}
	cleanup()
}
