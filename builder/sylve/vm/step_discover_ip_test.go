// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	sylvecommon "github.com/xoro/packer-plugin-sylve/builder/sylve/common"
)

func TestStepDiscoverIP_CleanupNoOp(t *testing.T) {
	step := &sylvecommon.StepDiscoverIP{}
	step.Cleanup(newTestState(t))
}

func TestStepDiscoverIP_NoMAC_Halt(t *testing.T) {
	step := &sylvecommon.StepDiscoverIP{}
	state := newTestState(t)
	// vm_mac not set in state.
	action := step.Run(context.Background(), state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt when vm_mac is empty, got %v", action)
	}
}

func TestStepDiscoverIP_LeaseFound(t *testing.T) {
	origTimeout := sylvecommon.DiscoverIPTotalTimeout
	origPoll := sylvecommon.DiscoverIPPollInterval
	sylvecommon.DiscoverIPTotalTimeout = 2 * time.Second
	sylvecommon.DiscoverIPPollInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		sylvecommon.DiscoverIPTotalTimeout = origTimeout
		sylvecommon.DiscoverIPPollInterval = origPoll
	})

	// /api/network/dhcp/leases — returns a lease for the given MAC.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/network/dhcp/lease", func(w http.ResponseWriter, _ *http.Request) {
		type lease struct {
			MAC string `json:"mac"`
			IP  string `json:"ip"`
		}
		type leases struct {
			File []lease `json:"file"`
		}
		resp := map[string]interface{}{
			"status": "success",
			"data":   leases{File: []lease{{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.42"}}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_mac", "AA:BB:CC:DD:EE:FF") // step normalises to lowercase

	step := &sylvecommon.StepDiscoverIP{SylveURL: cfg.SylveURL, SylveToken: cfg.SylveToken, TLSSkipVerify: cfg.TLSSkipVerify}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v; error=%v", action, state.Get("error"))
	}
	if ip, _ := state.Get("instance_ip").(string); ip != "10.0.0.42" {
		t.Errorf("instance_ip = %q, want %q", ip, "10.0.0.42")
	}
}

func TestStepDiscoverIP_Timeout_Halt(t *testing.T) {
	origTimeout := sylvecommon.DiscoverIPTotalTimeout
	origPoll := sylvecommon.DiscoverIPPollInterval
	sylvecommon.DiscoverIPTotalTimeout = 30 * time.Millisecond
	sylvecommon.DiscoverIPPollInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		sylvecommon.DiscoverIPTotalTimeout = origTimeout
		sylvecommon.DiscoverIPPollInterval = origPoll
	})

	// Return empty lease list so the step never finds a match.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/network/dhcp/lease", func(w http.ResponseWriter, _ *http.Request) {
		type leases struct {
			File []interface{} `json:"file"`
		}
		resp := map[string]interface{}{"status": "success", "data": leases{File: []interface{}{}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := &Config{SylveURL: srv.URL, TLSSkipVerify: true}
	state := newTestState(t)
	state.Put("vm_mac", "aa:bb:cc:dd:ee:ff")

	step := &sylvecommon.StepDiscoverIP{SylveURL: cfg.SylveURL, SylveToken: cfg.SylveToken, TLSSkipVerify: cfg.TLSSkipVerify}
	action := step.Run(context.Background(), state)
	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt on timeout, got %v", action)
	}
}
