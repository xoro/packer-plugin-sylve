// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"fmt"
	"log"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepFindVM looks up the existing VM by name in the Sylve registry, validates
// that it is not already running, and stores identifiers needed by downstream
// steps. The VM must be stopped before the build begins.
type StepFindVM struct {
	Config *Config
}

func (s *StepFindVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	ui.Say(fmt.Sprintf("Finding VM %q in Sylve registry...", s.Config.VMName))

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	vm, err := c.FindVMByName(s.Config.VMName)
	if err != nil {
		err = fmt.Errorf("find VM %q: %w", s.Config.VMName, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// The VM must be stopped before we snapshot or start it.
	if vm.State == client.DomainStateRunning {
		err = fmt.Errorf("VM %q (RID %d) is already running; stop it before running Packer", s.Config.VMName, vm.RID)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	state.Put("vm_rid", vm.RID)
	state.Put("vm_id", vm.ID)
	state.Put("vm_storages", vm.Storages)

	// Persist the first network interface MAC address for IP discovery (DHCP
	// lease lookup). Fall back to an empty string; StepDiscoverIP will skip the
	// lease poll and expect the communicator's static host to be set instead.
	// Use MACAddress() because the top-level MAC field is always empty in the
	// current Sylve API; the real value lives in MacObj.Entries[0].Value.
	mac := ""
	if len(vm.Networks) > 0 {
		mac = vm.Networks[0].MACAddress()
	}
	state.Put("vm_mac", mac)

	log.Printf("[DEBUG] sylve-vm: found VM %q RID=%d ID=%d MAC=%q storages=%d",
		s.Config.VMName, vm.RID, vm.ID, mac, len(vm.Storages))
	ui.Say(fmt.Sprintf("Found VM %q (RID %d)", s.Config.VMName, vm.RID))
	return multistep.ActionContinue
}

func (s *StepFindVM) Cleanup(_ multistep.StateBag) {}
