// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylvevm

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepShutdown shuts the VM down after provisioning. When shutdown_command is
// non-empty it is sent over the communicator (SSH or WinRM) first; otherwise
// Sylve's StopVM API is called directly. After the graceful or forced shutdown
// request the step polls until the domain is no longer running.
type StepShutdown struct {
	Config *Config
}

// shutdownPollInterval and shutdownMaxWait are overridable in tests.
var (
	shutdownPollInterval = 5 * time.Second
	shutdownMaxWait      = 5 * time.Minute
)

func (s *StepShutdown) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	rid, _ := state.Get("vm_rid").(uint)
	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	if s.Config.ShutdownCommand != "" {
		// Send graceful shutdown command over the communicator.
		comm, ok := state.GetOk("communicator")
		if !ok {
			err := fmt.Errorf("communicator not present in state bag; cannot send shutdown command")
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		communicator := comm.(packersdk.Communicator)

		cmd := &packersdk.RemoteCmd{
			Command: s.Config.ShutdownCommand,
			Stdout:  new(bytes.Buffer),
			Stderr:  new(bytes.Buffer),
		}

		ui.Say(fmt.Sprintf("Sending shutdown command: %s", s.Config.ShutdownCommand))
		if err := communicator.Start(ctx, cmd); err != nil {
			err = fmt.Errorf("run shutdown command: %w", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		cmd.Wait()
	}

	// Issue a Sylve-side stop to ensure bhyve is halted even if the guest
	// poweroff raced with the communicator exit, or when no shutdown_command
	// was configured. Errors are non-fatal: the VM may already be stopped.
	if err := c.StopVM(rid); err != nil {
		ui.Say(fmt.Sprintf("VM rid=%d Sylve stop: %s (may already be stopped)", rid, err))
	}

	// Wait for the domain to stop running.
	//
	// We poll GET /api/vm/simple/:rid which queries libvirt at request time and
	// returns the real runtime state. The full GET /api/vm/:rid endpoint always
	// returns State=0 (the field is not stored in the DB), so it cannot be used
	// here.
	ui.Say(fmt.Sprintf("Waiting for VM rid=%d to shut down...", rid))

	timeout := shutdownMaxWait
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(shutdownPollInterval):
		}

		if time.Now().After(deadline) {
			ui.Say(fmt.Sprintf("VM rid=%d did not shut down cleanly within %s; forcing stop", rid, timeout))
			if err := c.StopVM(rid); err != nil {
				ui.Error(fmt.Sprintf("Force stop VM rid=%d: %s", rid, err))
			}
			return multistep.ActionContinue
		}

		simpleVM, err := c.GetSimpleVMByRID(rid)
		if err != nil {
			if client.IsNotFound(err) {
				ui.Say(fmt.Sprintf("VM rid=%d no longer exists in Sylve; treating as stopped", rid))
				return multistep.ActionContinue
			}
			continue
		}

		// State=1 (Running) is the only state where the VM is actively using
		// CPU/memory. Any other value means the VM is no longer running.
		if simpleVM.State != client.DomainStateRunning {
			ui.Say(fmt.Sprintf("VM rid=%d is stopped (state=%d)", rid, simpleVM.State))
			return multistep.ActionContinue
		}
	}
}

func (s *StepShutdown) Cleanup(_ multistep.StateBag) {}
