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

// StepCreateFromTemplate creates a new VM from a named Sylve template.
// It auto-allocates a free RID, issues the create request, polls the lifecycle
// task until creation completes, then fetches the full VM state. On failure
// during cleanup the created VM is destroyed (it is disposable).
type StepCreateFromTemplate struct {
	Config *Config
}

func (s *StepCreateFromTemplate) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	// Find template by name.
	ui.Say(fmt.Sprintf("Looking up template %q...", s.Config.SourceTemplate))
	tmpl, err := c.FindTemplateByName(s.Config.SourceTemplate)
	if err != nil {
		err = fmt.Errorf("find template %q: %w", s.Config.SourceTemplate, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	ui.Say(fmt.Sprintf("Found template %q (ID %d)", tmpl.Name, tmpl.ID))

	// Allocate a free RID for the new VM.
	rid, err := c.FindNextFreeRID()
	if err != nil {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	log.Printf("[DEBUG] sylve-vm: allocated RID %d for new VM", rid)

	// Create VM from template.
	ui.Say(fmt.Sprintf("Creating VM %q from template %q (RID %d)...", s.Config.VMName, tmpl.Name, rid))
	req := client.CreateFromTemplateRequest{
		Name: s.Config.VMName,
		RID:  rid,
	}
	if err := c.CreateVMFromTemplate(tmpl.ID, req); err != nil {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Poll until the VM appears and the lifecycle task completes.
	// The template create endpoint is async; wait for the VM to show up at the
	// allocated RID.
	ui.Say("Waiting for VM creation to complete...")
	vm, err := s.waitForVM(c, rid)
	if err != nil {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Store state for downstream steps.
	state.Put("vm_rid", vm.RID)
	state.Put("vm_id", vm.ID)
	state.Put("vm_storages", vm.Storages)

	mac := ""
	if len(vm.Networks) > 0 {
		mac = vm.Networks[0].MACAddress()
	}
	state.Put("vm_mac", mac)

	// Store NIC metadata for the downstream StepFixNIC. The NIC fix cannot run
	// here because the libvirt domain does not exist until the first VM start.
	if len(vm.Networks) > 0 {
		state.Put("vm_network_id", vm.Networks[0].ID)
		state.Put("vm_network_emulation", vm.Networks[0].Emulation)
		state.Put("vm_network_switch", vm.Networks[0].SwitchName)
		// Preserve macId so StepFixNIC can reattach with the same MAC for
		// Windows guests (WinRM config is bound to the adapter MAC).
		if vm.Networks[0].MacID != nil {
			state.Put("vm_network_mac_id", *vm.Networks[0].MacID)
		}
	}

	log.Printf("[DEBUG] sylve-vm: created VM %q RID=%d ID=%d MAC=%q storages=%d",
		s.Config.VMName, vm.RID, vm.ID, mac, len(vm.Storages))
	ui.Say(fmt.Sprintf("VM %q created (RID %d)", s.Config.VMName, vm.RID))
	return multistep.ActionContinue
}

// Polling intervals for waitForVM (variables so tests can override).
var (
	createFromTemplatePollInterval = 3 * time.Second
	createFromTemplateMaxWait      = 5 * time.Minute
)

// waitForVM polls GET /api/vm/:rid until the VM exists and has no active
// lifecycle task (creation complete). Times out after createFromTemplateMaxWait.
func (s *StepCreateFromTemplate) waitForVM(c *client.Client, rid uint) (*client.VM, error) {
	timeout := createFromTemplateMaxWait
	deadline := time.Now().Add(timeout)
	poll := createFromTemplatePollInterval

	for {
		vm, err := c.GetVMByRID(rid)
		if err == nil {
			// Check if the lifecycle task is still running.
			active, taskErr := c.HasActiveLifecycleTask(vm.ID)
			if taskErr != nil {
				log.Printf("[DEBUG] sylve-vm: lifecycle task check error: %v", taskErr)
			}
			if !active {
				return vm, nil
			}
			log.Printf("[DEBUG] sylve-vm: lifecycle task still active for VM ID=%d, waiting...", vm.ID)
		} else {
			log.Printf("[DEBUG] sylve-vm: VM RID=%d not yet available: %v", rid, err)
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out after %v waiting for VM RID=%d to be created", timeout, rid)
		}
		time.Sleep(poll)
	}
}

// Cleanup destroys the created VM when the build failed. The VM is disposable
// since it was freshly cloned from a template.
func (s *StepCreateFromTemplate) Cleanup(state multistep.StateBag) {
	_, cancelled := state.GetOk(multistep.StateCancelled)
	_, halted := state.GetOk(multistep.StateHalted)
	if !cancelled && !halted {
		return
	}

	vmRID, _ := state.Get("vm_rid").(uint)
	if vmRID == 0 {
		return
	}

	ui := state.Get("ui").(packersdk.Ui)
	ui.Say(fmt.Sprintf("Cleaning up: deleting VM RID=%d (build failed)...", vmRID))

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)
	if err := c.DeleteVM(vmRID); err != nil {
		ui.Error(fmt.Sprintf("Cleanup: failed to delete VM RID=%d: %s", vmRID, err))
	} else {
		ui.Say(fmt.Sprintf("Cleanup: VM RID=%d deleted", vmRID))
		state.Put("vm_rid", uint(0))
	}
}
