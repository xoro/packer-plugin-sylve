// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package iso

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

// TestStepCreateVM_Run_SendsIgnoreUMSRs proves that ignore_umsrs reaches
// Sylve's create request as configured: OpenBSD 8.0 panics on bhyve without it.
func TestStepCreateVM_Run_SendsIgnoreUMSRs(t *testing.T) {
	for _, want := range []bool{true, false} {
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			restoreCreateVMDurations(t)
			port := newFreeTCPPort(t)
			flow := serveCreateVMFlow(t, "packer-test", 7, 100)
			defer flow.Close()
			target, err := url.Parse(flow.URL)
			if err != nil {
				t.Fatal(err)
			}
			proxy := httputil.NewSingleHostReverseProxy(target)

			var mu sync.Mutex
			var sent map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/vm" && r.Method == http.MethodPost {
					body, _ := io.ReadAll(r.Body)
					mu.Lock()
					_ = json.Unmarshal(body, &sent)
					mu.Unlock()
					r.Body = io.NopCloser(bytes.NewReader(body))
				}
				proxy.ServeHTTP(w, r)
			}))
			defer srv.Close()

			step := &StepCreateVM{Config: &Config{
				SylveURL:             srv.URL,
				SylveToken:           "tok",
				TLSSkipVerify:        true,
				VMName:               "packer-test",
				StoragePool:          "tank",
				SwitchName:           "sw",
				VNCPortMin:           port,
				VNCPortMax:           port,
				VNCHost:              "127.0.0.1",
				StorageSizeMB:        1024,
				StorageType:          "zvol",
				StorageEmulationType: "virtio-blk",
				SwitchEmulationType:  "e1000",
				IgnoreUMSRs:          want,
			}}
			state := new(multistep.BasicStateBag)
			state.Put("ui", newMockUI())
			state.Put("iso_uuid", "iso-uuid")

			if got := step.Run(context.Background(), state); got != multistep.ActionContinue {
				t.Fatalf("Run() = %v, want ActionContinue", got)
			}
			mu.Lock()
			defer mu.Unlock()
			if got, ok := sent["ignoreUMSR"].(bool); !ok || got != want {
				t.Fatalf("create request ignoreUMSR = %#v, want %v", sent["ignoreUMSR"], want)
			}
		})
	}
}
