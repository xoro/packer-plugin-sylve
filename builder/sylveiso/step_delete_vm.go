// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"fmt"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepDeleteVM deletes the VM from Sylve on the success path if Config.Destroy
// is true. On error or cancellation the deletion is handled by StepCreateVM.Cleanup.
type StepDeleteVM struct {
	Config *Config
}

func (s *StepDeleteVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	if !s.Config.Destroy {
		return multistep.ActionContinue
	}

	ui := state.Get("ui").(packersdk.Ui)
	vmRID, _ := state.Get("vm_rid").(uint)
	if vmRID == 0 {
		return multistep.ActionContinue
	}

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	ui.Say(fmt.Sprintf("Deleting VM rid=%d (destroy=true)...", vmRID))
	if err := c.DeleteVM(vmRID); err != nil {
		ui.Error(fmt.Sprintf("Delete VM rid=%d: %s (continuing anyway)", vmRID, err))
	} else {
		ui.Say(fmt.Sprintf("VM rid=%d deleted", vmRID))
		// Zero vm_rid so StepCreateVM.Cleanup knows deletion is already done.
		state.Put("vm_rid", uint(0))
	}

	return multistep.ActionContinue
}

func (s *StepDeleteVM) Cleanup(_ multistep.StateBag) {}
