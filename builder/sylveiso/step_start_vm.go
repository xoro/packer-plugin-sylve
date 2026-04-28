// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepStartVM starts the VM via the Sylve API and polls until the domain
// reaches the Running state. The same step struct is embedded twice in the
// builder step sequence: once for Phase A (installer boot) and once for
// Phase B (provision boot after install).
type StepStartVM struct {
	Config *Config
}

// startVMPollInterval and startVMMaxWait are overridable in tests.
var (
	startVMPollInterval      = 3 * time.Second
	startVMMaxWait           = 15 * time.Minute
	startVMTaskPoll          = 3 * time.Second
	startVMTaskMaxWait       = 3 * time.Minute
	startVMStartRetry        = 3 * time.Second
	startVMStartRetryMaxWait = 2 * time.Minute
)

func (s *StepStartVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	rid, _ := state.Get("vm_rid").(uint)

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	// Release the local TCP listener on the VNC port held open since VM creation.
	// Bhyve binds the same address (127.0.0.1:VNCPort) when it starts; if the
	// plugin's listener is still open bhyve fails immediately with
	// "Device emulation initialization error: Address already in use".
	// StepVNCBootCommand binds a fresh listener for the view server after the
	// VNC WebSocket connection is established.
	if ln, ok := state.GetOk("vnc_view_listener"); ok {
		if listener, ok := ln.(net.Listener); ok {
			_ = listener.Close()
			state.Remove("vnc_view_listener")
		}
	}

	// Wait for any active lifecycle task (e.g. ZFS zvol creation triggered by
	// POST /api/vm) to finish before calling StartVM. Sylve returns 409
	// lifecycle_task_in_progress when the zvol provisioning is still running.
	taskDeadline := time.Now().Add(startVMTaskMaxWait)
	for {
		if vmID, ok := state.Get("vm_id").(uint); ok {
			active, err := c.HasActiveLifecycleTask(vmID)
			if err == nil && !active {
				break
			}
			if err != nil {
				log.Printf("[DEBUG] start VM: lifecycle task poll error: %s", err)
			}
		} else {
			break
		}
		if time.Now().After(taskDeadline) {
			log.Printf("[DEBUG] start VM: lifecycle task still active after 3 minutes; proceeding anyway")
			break
		}
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(startVMTaskPoll):
		}
	}

	// Sylve may still briefly return 409 after the task poll clears, so retry.
	ui.Say(fmt.Sprintf("Starting VM rid=%d...", rid))
	startRetryDeadline := time.Now().Add(startVMStartRetryMaxWait)
	for {
		err := c.StartVM(rid)
		if err == nil {
			break
		}
		if time.Now().After(startRetryDeadline) {
			err = fmt.Errorf("start VM: %w", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		log.Printf("[DEBUG] start VM rid=%d: %s — retrying in 3s", rid, err)
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(startVMStartRetry):
		}
	}

	timeout := startVMMaxWait
	deadline := time.Now().Add(timeout)
	var lastState client.DomainState = -1
	for {
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(startVMPollInterval):
		}

		if time.Now().After(deadline) {
			err := fmt.Errorf("VM rid=%d did not reach Running state within %s", rid, timeout)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		vm, err := c.GetSimpleVMByRID(rid)
		if err != nil {
			ui.Say(fmt.Sprintf("Waiting for VM rid=%d (poll error: %s)", rid, err))
			continue
		}
		if vm.State != lastState {
			log.Printf("[DEBUG] VM rid=%d state: %d", rid, vm.State)
			lastState = vm.State
		}
		if vm.State == client.DomainStateRunning || vm.State == client.DomainStateBlocked {
			ui.Say(fmt.Sprintf("VM rid=%d is running", rid))
			if logs, err := c.GetVMLogs(rid); err == nil && logs != "" {
				log.Printf("[DEBUG] VM rid=%d bhyve log:\n%s", rid, logs)
			}
			return multistep.ActionContinue
		}
	}
}

func (s *StepStartVM) Cleanup(_ multistep.StateBag) {}
