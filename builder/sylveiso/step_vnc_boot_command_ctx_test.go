// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

// TestStepVNCBootCommand_Run_ContextCancelDuringDialRetry stops when the build
// context is cancelled while waiting between WebSocket dial attempts.
func TestStepVNCBootCommand_Run_ContextCancelDuringDialRetry(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	cfg := &Config{
		SylveURL:        srv.URL,
		SylveToken:      "test-token",
		VNCPort:         5900,
		VNCHost:         "127.0.0.1",
		TLSSkipVerify:   true,
		BootWait:        "1ns",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}

	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vnc_view_listener", ln)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(400 * time.Millisecond)
		cancel()
	}()

	if got := step.Run(ctx, state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
	err, ok := state.Get("error").(error)
	if !ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", state.Get("error"))
	}
}

func TestStepVNCBootCommand_Run_BootCommandInterpolateError(t *testing.T) {
	cfg := &Config{
		SylveURL:    "https://127.0.0.1:9",
		SylveToken:  "t",
		VNCHost:     "h",
		VNCPort:     5900,
		BootCommand: []string{"{{"},
	}
	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}

func TestStepVNCBootCommand_Run_DialDeadlineExhausted(t *testing.T) {
	origD, origR := vncStepOverallDeadline, vncStepDialRetryDelay
	vncStepOverallDeadline = 0
	vncStepDialRetryDelay = 0
	t.Cleanup(func() {
		vncStepOverallDeadline = origD
		vncStepDialRetryDelay = origR
	})

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := &Config{
		SylveURL:        srv.URL,
		SylveToken:      "test-token",
		VNCPort:         5900,
		VNCHost:         "127.0.0.1",
		TLSSkipVerify:   true,
		BootWait:        "1ns",
		BootCommand:     []string{"<wait1ms>"},
		BootKeyInterval: 1 * time.Millisecond,
	}
	step := &StepVNCBootCommand{Config: cfg}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}
