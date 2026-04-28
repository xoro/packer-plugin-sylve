// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// ---------------------------------------------------------------------------
// StepDiscoverIP
// ---------------------------------------------------------------------------

// TestStepDiscoverIP_Run_ViaDHCPLease covers the Sylve DHCP lease API success path.
// StepDiscoverIP waits 10s before the first poll; skipped under -short.
func TestStepDiscoverIP_Run_ViaDHCPLease(t *testing.T) {
	oldT, oldP := discoverIPTotalTimeout, discoverIPPollInterval
	discoverIPTotalTimeout = 5 * time.Second
	discoverIPPollInterval = 1 * time.Millisecond
	t.Cleanup(func() {
		discoverIPTotalTimeout = oldT
		discoverIPPollInterval = oldP
	})
	const mac = "aa:bb:cc:dd:ee:ff"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/network/dhcp/lease" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
				Status: "ok",
				Data: client.Leases{
					File: []client.FileLease{{MAC: mac, IP: "10.0.0.42"}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", mac)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v, want ActionContinue", got)
	}
	if state.Get("instance_ip") != "10.0.0.42" {
		t.Fatalf("instance_ip = %v", state.Get("instance_ip"))
	}
}

// TestStepDiscoverIP_DHCPPollErrorThenLease covers the Sylve DHCP API error log
// path followed by a successful lease on a later poll.
func TestStepDiscoverIP_DHCPPollErrorThenLease(t *testing.T) {
	oldT, oldP := discoverIPTotalTimeout, discoverIPPollInterval
	discoverIPTotalTimeout = 5 * time.Second
	discoverIPPollInterval = 1 * time.Millisecond
	t.Cleanup(func() {
		discoverIPTotalTimeout = oldT
		discoverIPPollInterval = oldP
	})
	const mac = "aa:bb:cc:dd:ee:ff"
	var getN int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/network/dhcp/lease" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			n := atomic.AddInt32(&getN, 1)
			if n == 1 {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
				Status: "ok",
				Data: client.Leases{
					File: []client.FileLease{{MAC: mac, IP: "10.0.0.77"}},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", mac)

	if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
		t.Fatalf("Run() = %v", got)
	}
	if state.Get("instance_ip") != "10.0.0.77" {
		t.Fatalf("instance_ip = %v", state.Get("instance_ip"))
	}
	if getN < 2 {
		t.Fatalf("expected at least 2 DHCP API calls, got %d", getN)
	}
}

func TestStepDiscoverIP_TotalTimeout(t *testing.T) {
	oldT, oldP := discoverIPTotalTimeout, discoverIPPollInterval
	discoverIPTotalTimeout = 0
	discoverIPPollInterval = 0
	t.Cleanup(func() {
		discoverIPTotalTimeout = oldT
		discoverIPPollInterval = oldP
	})

	const mac = "aa:bb:cc:dd:ee:ff"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/network/dhcp/lease" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.APIResponse[client.Leases]{
				Status: "ok",
				Data:   client.Leases{File: []client.FileLease{}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	step := &StepDiscoverIP{Config: &Config{SylveURL: srv.URL, SylveToken: "tok", TLSSkipVerify: true}}
	state := new(multistep.BasicStateBag)
	state.Put("ui", newMockUI())
	state.Put("vm_mac", mac)

	if got := step.Run(context.Background(), state); got != multistep.ActionHalt {
		t.Fatalf("Run() = %v, want ActionHalt", got)
	}
}
