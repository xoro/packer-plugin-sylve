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

// Step restart timings — tests shorten these to avoid multi-minute sleeps.
var (
	restartAfterInstallShutoffPoll    = 3 * time.Second
	restartAfterInstallShutoffMaxWait = 3 * time.Minute
	restartAfterInstallTaskPoll       = 3 * time.Second
	restartAfterInstallTaskMaxWait    = 3 * time.Minute
	restartAfterInstallStartRetry     = 3 * time.Second
	restartAfterInstallStartMaxWait   = 5 * time.Minute
	restartAfterInstallRunningPoll    = 3 * time.Second
	restartAfterInstallRunningMaxWait = 3 * time.Minute
)

// StepRestartAfterInstall waits for the installer VM to stop (the Alpine
// installer ends with a guest-initiated "reboot", which causes bhyve to exit
// and the VM to transition to Shutoff), then starts it again so it boots into
// the freshly installed OS for provisioning via SSH.
//
// A StopVM call is issued first as a safety net in case the boot commands
// finish before the guest reboot has completed; if the VM already stopped the
// call is a no-op (Sylve logs nothing or returns an ignorable error).
//
// With startAtBoot=false (set during VM creation) Sylve will NOT auto-restart
// the VM after the guest reboot, so the plugin retains full control of the
// second start and can disable the ISO storage before issuing it.
type StepRestartAfterInstall struct {
	Config *Config
}

func (s *StepRestartAfterInstall) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	rid, _ := state.Get("vm_rid").(uint)

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	// Sylve's bhyve supervisor auto-restarts bhyve when the guest calls reboot,
	// without updating stoppedAt. By the time this step runs the installer has
	// already finished (the VNC reconnect during boot_command confirmed it) and
	// bhyve is running the installed OS under the same Sylve VM record. Polling
	// for a natural stop is futile; just force-stop the running VM directly.
	log.Printf("[DEBUG] Force-stopping installer VM rid=%d...", rid)
	stopIssuedAt := time.Now()
	if err := c.StopVM(rid); err != nil {
		log.Printf("[DEBUG] Force-stop VM rid=%d: %s (may already be stopped)", rid, err)
	}

	// Wait for Bhyve to exit and Sylve to report Shutoff before restarting.
	// Sylve's lifecycle task cleanup (VNC port teardown, QGA socket removal) can
	// take up to 3 minutes in practice before /vm/start accepts a 200 response.
	// We compare stoppedAt against stopIssuedAt: when stoppedAt is after the
	// moment we issued StopVM, the domain has definitively halted.
	// The state field is not used because GetVMByRID always returns state=0
	// (State has gorm:"-" and is never populated from the DB).
	shutoffDeadline := time.Now().Add(restartAfterInstallShutoffMaxWait)
shutoffLoop:
	for {
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(restartAfterInstallShutoffPoll):
		}

		vm, err := c.GetVMByRID(rid)
		if err != nil {
			if time.Now().After(shutoffDeadline) {
				break shutoffLoop
			}
			continue
		}
		if !vm.StoppedAt.IsZero() && vm.StoppedAt.After(stopIssuedAt) {
			log.Printf("[DEBUG] VM rid=%d is stopped (stoppedAt: %s)", rid, vm.StoppedAt.Format(time.RFC3339))
			break shutoffLoop
		}
		if time.Now().After(shutoffDeadline) {
			log.Printf("[DEBUG] VM rid=%d still not stopped after 3 minutes; proceeding anyway", rid)
			break shutoffLoop
		}
	}

	// Disable the ISO storage before restarting.
	//
	// Although the bhyve slot order puts the zvol before the CD, the UEFI
	// NVRAM file (86_vars.fd) is persistent across VM starts.  On the first
	// boot the CD is the only bootable device, so EDK2 writes it as Boot0000
	// in the vars file.  On the second start EDK2 honours that stored entry
	// and boots from the CD again, even though Alpine has now installed a
	// bootloader on the zvol.
	//
	// Disabling the ISO causes Sylve's SyncVMDisks to omit the CD from the
	// bhyve command line entirely, so EDK2 has no choice but to boot the zvol.
	if isoID, ok := state.Get("iso_storage_id").(int); ok && isoID != 0 {
		isoName, _ := state.Get("iso_storage_name").(string)
		isoEmulation, _ := state.Get("iso_storage_emulation").(string)
		ui.Say(fmt.Sprintf("Disabling ISO storage id=%d before installed-OS boot...", isoID))
		if err := c.DisableISOStorage(isoID, isoName, isoEmulation); err != nil {
			// Non-fatal: log and proceed; worst case we get a CD-boot again.
			ui.Say(fmt.Sprintf("Warning: could not disable ISO storage id=%d: %s", isoID, err))
		}
	}

	// vncWait is always set to false in CreateVMRequest, so no VNC wait
	// configuration change is needed before the installed-OS start.

	// Release the local TCP listener on the VNC port that was held open since
	// VM creation. Bhyve binds the same address (127.0.0.1:VNCPort) when it
	// starts; if the plugin's listener is still open bhyve fails immediately,
	// the VM never reaches Running state, and the build times out.
	if ln, ok := state.GetOk("vnc_view_listener"); ok {
		if listener, ok := ln.(net.Listener); ok {
			_ = listener.Close()
			state.Remove("vnc_view_listener")
		}
	}

	// Start the VM so it boots into the installed OS.
	// Wait for any active lifecycle task (e.g. the preceding force-stop cleanup)
	// to finish before calling StartVM. Sylve's stop task tears down VNC sockets
	// and TAP interfaces asynchronously; calling StartVM while it is still
	// running causes bhyve to fail immediately with a port-already-in-use or
	// similar error, leaving the VM at state 0. Sylve reports 409 for some
	// races but silently fails for others, so poll the task API directly.
	taskDeadline := time.Now().Add(restartAfterInstallTaskMaxWait)
	ui.Say(fmt.Sprintf("Waiting for Sylve lifecycle tasks to clear for VM id=%d...", state.Get("vm_id")))
	for {
		if vmID, ok := state.Get("vm_id").(uint); ok {
			active, err := c.HasActiveLifecycleTask(vmID)
			if err == nil && !active {
				break
			}
			if err != nil {
				ui.Say(fmt.Sprintf("Warning: task poll error: %s", err))
			}
		} else {
			break
		}
		if time.Now().After(taskDeadline) {
			ui.Say("Lifecycle task still active after 3 minutes; proceeding anyway")
			break
		}
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(restartAfterInstallTaskPoll):
		}
	}

	// Sylve may still report 409 lifecycle_task_in_progress briefly after the
	// task poll clears, so retry until it accepts the start or we time out.
	startDeadline := time.Now().Add(restartAfterInstallStartMaxWait)
	for {
		err := c.StartVM(rid)
		if err == nil {
			break
		}
		if time.Now().After(startDeadline) {
			err = fmt.Errorf("restart VM after install: %w", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		ui.Say(fmt.Sprintf("VM rid=%d start pending (lifecycle task in progress), retrying in 3s...", rid))
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(restartAfterInstallStartRetry):
		}
	}

	// Wait for the VM to reach Running state before StepDiscoverIP begins polling.
	runDeadline := time.Now().Add(restartAfterInstallRunningMaxWait)
	var lastState client.DomainState = -1
	for {
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(restartAfterInstallRunningPoll):
		}

		if time.Now().After(runDeadline) {
			err := fmt.Errorf("VM rid=%d did not reach Running state within 3 minutes after restart", rid)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		vm, err := c.GetSimpleVMByRID(rid)
		if err != nil {
			ui.Say(fmt.Sprintf("Waiting for VM rid=%d to start (poll error: %s)", rid, err))
			continue
		}

		if vm.State != lastState {
			log.Printf("[DEBUG] VM rid=%d state: %d", rid, vm.State)
			lastState = vm.State
		}

		if vm.State == client.DomainStateRunning || vm.State == client.DomainStateBlocked {
			log.Printf("[DEBUG] VM rid=%d is running", rid)
			// Reconnect the VNC view server to the new bhyve instance so a
			// connected viewer can watch the installed-OS boot.  This runs
			// after StartVM confirms the new bhyve is accepting connections,
			// which is the earliest reliable moment to dial the VNC WebSocket.
			if rfn, ok := state.GetOk("vnc_reconnect"); ok {
				if fn, ok := rfn.(vncReconnectFunc); ok {
					log.Printf("[DEBUG] Reconnecting VNC view server to installed-OS bhyve...")
					if err := fn(ctx, ui); err != nil {
						log.Printf("[DEBUG] VNC reconnect (non-fatal): %s", err)
					}
				}
			}
			return multistep.ActionContinue
		}
	}
}

func (s *StepRestartAfterInstall) Cleanup(_ multistep.StateBag) {}
