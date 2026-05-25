// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package common

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepDeleteVM deletes the VM from Sylve on the success path if Destroy is
// true. When Destroy is false this step is a no-op.
type StepDeleteVM struct {
	SylveURL      string
	SylveToken    string
	TLSSkipVerify bool
	Destroy       bool
}

func (s *StepDeleteVM) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	if !s.Destroy {
		return multistep.ActionContinue
	}

	ui := state.Get("ui").(packersdk.Ui)
	vmRID, _ := state.Get("vm_rid").(uint)
	if vmRID == 0 {
		return multistep.ActionContinue
	}

	c := client.New(s.SylveURL, s.SylveToken, s.TLSSkipVerify)

	// Fetch and log the VM's storage devices before deletion so the pre-delete
	// inventory can be correlated against 'zfs list' output after the run.
	// This is the primary diagnostic for the Sylve bug where deletevolumes=true
	// silently fails to destroy ZFS datasets (visible with PACKER_LOG=1).
	if vm, err := c.GetVMByRID(vmRID); err != nil {
		log.Printf("[DEBUG] StepDeleteVM: could not fetch VM rid=%d for storage inventory: %v", vmRID, err)
	} else {
		log.Printf("[DEBUG] StepDeleteVM: VM rid=%d id=%d name=%q has %d storage device(s) to delete",
			vmRID, vm.ID, vm.Name, len(vm.Storages))
		for _, st := range vm.Storages {
			if st.Dataset != nil {
				log.Printf("[DEBUG] StepDeleteVM: storage id=%d type=%s name=%s pool=%s dataset=%s/%s",
					st.ID, st.Type, st.Name, st.Pool, st.Dataset.Pool, st.Dataset.Name)
			} else {
				log.Printf("[DEBUG] StepDeleteVM: storage id=%d type=%s name=%s pool=%s (no ZFS dataset record)",
					st.ID, st.Type, st.Name, st.Pool)
			}
		}
	}

	ui.Say(fmt.Sprintf("Deleting VM rid=%d (destroy=true)...", vmRID))
	if err := c.DeleteVM(vmRID); err != nil {
		ui.Error(fmt.Sprintf("Delete VM rid=%d: %s (continuing anyway)", vmRID, err))
	} else {
		ui.Say(fmt.Sprintf("VM rid=%d deleted", vmRID))
		// Zero vm_rid so any cleanup code knows deletion is already done.
		state.Put("vm_rid", uint(0))
	}

	return multistep.ActionContinue
}

func (s *StepDeleteVM) Cleanup(_ multistep.StateBag) {}
