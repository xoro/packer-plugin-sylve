// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// StepBootWait waits for the duration configured in boot_wait before
// proceeding to the communicator connect step. This is useful for Windows
// guests that require time after the VM's DHCP lease is visible before
// WinRM or SSH is responsive.
//
// The step is a no-op when boot_wait is empty or zero.
type StepBootWait struct {
	Config *Config
}

func (s *StepBootWait) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)

	d, err := s.Config.bootWaitDuration()
	if err != nil {
		state.Put("error", fmt.Errorf("boot_wait: %w", err))
		return multistep.ActionHalt
	}
	if d == 0 {
		return multistep.ActionContinue
	}

	ui.Say(fmt.Sprintf("Waiting %s before connecting (boot_wait)...", d))
	select {
	case <-time.After(d):
	case <-ctx.Done():
		return multistep.ActionHalt
	}
	return multistep.ActionContinue
}

func (s *StepBootWait) Cleanup(_ multistep.StateBag) {}
