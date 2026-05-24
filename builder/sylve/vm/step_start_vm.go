// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepStartVM starts the VM via the Sylve API and polls until the domain
// reaches the Running state.
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
	startVMCleanupMaxWait    = 3 * time.Minute
)

func (s *StepStartVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	rid, _ := state.Get("vm_rid").(uint)

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

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
	// Count consecutive Shutoff/Crashed polls to distinguish a transient
	// startup state from a genuine bhyve crash.
	var crashPollCount int
	const crashPollThreshold = 3
	// Count consecutive NoState polls to detect a VM that never started.
	// bhyve may briefly report NoState during normal startup, so a higher
	// threshold is used.
	var noStatePollCount int
	const noStatePollThreshold = 10
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

		// Detect bhyve crash: if the VM stays at Shutoff (5) or Crashed (6)
		// for several consecutive polls, bhyve exited immediately. Fail fast
		// and show the bhyve log instead of polling until the 15-minute timeout.
		// A single Shutoff is tolerated because bhyve may briefly pass through
		// this state during normal startup.
		if vm.State == client.DomainStateShutoff || vm.State == client.DomainStateCrashed {
			crashPollCount++
			if crashPollCount >= crashPollThreshold {
				bhyveLogs := ""
				if logs, logErr := c.GetVMLogs(rid); logErr == nil {
					bhyveLogs = logs
				}
				err := fmt.Errorf("VM rid=%d crashed on start (state=%d, stable for %d polls). bhyve log:\n%s",
					rid, vm.State, crashPollCount, bhyveLogs)
				state.Put("error", err)
				ui.Error(err.Error())
				return multistep.ActionHalt
			}
		} else {
			crashPollCount = 0
		}

		// Detect stuck NoState: if the VM stays at NoState (0) for many
		// consecutive polls, bhyve never launched. This typically means
		// the domain could not be created (e.g. NIC misconfiguration).
		if vm.State == client.DomainStateNoState {
			noStatePollCount++
			if noStatePollCount >= noStatePollThreshold {
				bhyveLogs := ""
				if logs, logErr := c.GetVMLogs(rid); logErr == nil {
					bhyveLogs = logs
				}
				err := fmt.Errorf("VM rid=%d stuck in NoState (state=0) for %d consecutive polls (%s); bhyve never started. bhyve log:\n%s",
					rid, noStatePollCount, time.Duration(noStatePollCount)*startVMPollInterval, bhyveLogs)
				state.Put("error", err)
				ui.Error(err.Error())
				return multistep.ActionHalt
			}
		} else {
			noStatePollCount = 0
		}
	}
}

// Cleanup stops the VM if it is still running so that subsequent cleanup steps
// (e.g. StepCreateFromTemplate deleting the VM) can operate on a stopped domain.
func (s *StepStartVM) Cleanup(state multistep.StateBag) {
	rid, ok := state.Get("vm_rid").(uint)
	if !ok || rid == 0 {
		return
	}

	ui := state.Get("ui").(packersdk.Ui)
	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	vm, err := c.GetSimpleVMByRID(rid)
	if err != nil {
		log.Printf("[DEBUG] step_start_vm cleanup: cannot query VM rid=%d state: %s", rid, err)
		return
	}
	if vm.State != client.DomainStateRunning && vm.State != client.DomainStateBlocked {
		return
	}

	ui.Say(fmt.Sprintf("Stopping VM rid=%d before snapshot rollback...", rid))
	if err := c.StopVM(rid); err != nil {
		log.Printf("[ERROR] step_start_vm cleanup: stop VM rid=%d: %s", rid, err)
		ui.Error(fmt.Sprintf("Failed to stop VM rid=%d: %s — snapshot rollback may fail", rid, err))
		return
	}

	// Poll until the VM leaves the running/blocked state so ZFS can proceed.
	deadline := time.Now().Add(startVMCleanupMaxWait)
	for time.Now().Before(deadline) {
		time.Sleep(startVMPollInterval)
		current, err := c.GetSimpleVMByRID(rid)
		if err != nil {
			log.Printf("[DEBUG] step_start_vm cleanup: poll error: %s", err)
			continue
		}
		if current.State != client.DomainStateRunning && current.State != client.DomainStateBlocked {
			log.Printf("[DEBUG] step_start_vm cleanup: VM rid=%d stopped", rid)
			return
		}
	}
	log.Printf("[WARN] step_start_vm cleanup: VM rid=%d did not stop within %s", rid, startVMCleanupMaxWait)
	ui.Error(fmt.Sprintf("VM rid=%d did not stop within %s — snapshot rollback may fail", rid, startVMCleanupMaxWait))
}
