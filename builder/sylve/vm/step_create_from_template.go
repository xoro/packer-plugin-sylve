// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	sylvecommon "github.com/xoro/packer-plugin-sylve/builder/sylve/common"
	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepCreateFromTemplate creates a new VM from a named Sylve template.
// It picks a free RID at random, issues the create request, polls the lifecycle
// task until creation completes, then fetches the full VM state. On failure
// during cleanup the created VM is destroyed (it is disposable).
//
// Builds started together can pick the same RID: Sylve keeps a RID and a name
// to one VM, and the step goes on only with the VM at its RID that bears its
// own name. Another VM there, or Sylve refusing the RID, means another build
// won it: the step tries another RID, up to maxCreateAttempts times.
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

	// Create VM from template, under a name Sylve accepts.
	vmName := sylvecommon.VMName(s.Config.VMName)
	var vm *client.VM
	for attempt := 1; ; attempt++ {
		free, err := c.FreeRIDs()
		if err != nil {
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		rid := pickRID(free)
		log.Printf("[DEBUG] sylve-vm: picked RID %d for new VM (attempt %d)", rid, attempt)
		ui.Say(fmt.Sprintf("Creating VM %q from template %q (RID %d)...", vmName, tmpl.Name, rid))
		err = c.CreateVMFromTemplate(tmpl.ID, client.CreateFromTemplateRequest{Name: vmName, RID: rid})
		if err == nil {
			// The template create endpoint is async: wait for the VM to show up at
			// the RID and its lifecycle task to complete.
			ui.Say("Waiting for VM creation to complete...")
			vm, err = s.waitForVM(c, rid, vmName)
		}
		if err == nil {
			break
		}
		if attempt < maxCreateAttempts && ridTaken(err) {
			ui.Say(fmt.Sprintf("RID %d went to another VM; trying another RID (%d of %d)", rid, attempt+1, maxCreateAttempts))
			continue
		}
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
		vmName, vm.RID, vm.ID, mac, len(vm.Storages))
	ui.Say(fmt.Sprintf("VM %q created (RID %d)", vmName, vm.RID))
	return multistep.ActionContinue
}

// maxCreateAttempts bounds the RIDs tried when other VMs take the picked ones.
const maxCreateAttempts = 5

// pickRID chooses one of the free RIDs at random, so that builds started
// together seldom pick the same one (a variable so tests can fix the choice).
// Without randomness it takes the first: the name check keeps that correct.
var pickRID = func(free []uint) uint {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(free))))
	if err != nil {
		return free[0]
	}
	return free[n.Int64()]
}

// errRIDTaken: the VM at the picked RID is another's.
var errRIDTaken = errors.New("RID taken by another VM")

// ridTaken tells whether another VM won the RID: its VM is there, or Sylve
// refused the RID as used (a VM or a jail holds it).
func ridTaken(err error) bool {
	return errors.Is(err, errRIDTaken) || strings.Contains(err.Error(), "rid_range_contains_used_values")
}

// Polling intervals for waitForVM (variables so tests can override).
var (
	createFromTemplatePollInterval = 3 * time.Second
	createFromTemplateMaxWait      = 5 * time.Minute
)

// waitForVM polls GET /api/vm/:rid until the VM exists and has no active
// lifecycle task (creation complete). A VM there under another name than name
// is another's: errRIDTaken. Times out after createFromTemplateMaxWait.
func (s *StepCreateFromTemplate) waitForVM(c *client.Client, rid uint, name string) (*client.VM, error) {
	timeout := createFromTemplateMaxWait
	deadline := time.Now().Add(timeout)
	poll := createFromTemplatePollInterval

	for {
		vm, err := c.GetVMByRID(rid)
		if err == nil && vm.Name != name {
			return nil, fmt.Errorf("%w: RID %d is VM %q, not %q", errRIDTaken, rid, vm.Name, name)
		}
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

	// Log the storage devices recorded at VM creation time so the pre-delete
	// inventory can be correlated against 'zfs list' after the run.
	if storages, ok := state.Get("vm_storages").([]client.VMStorage); ok {
		log.Printf("[DEBUG] StepCreateFromTemplate cleanup: VM rid=%d has %d storage device(s)", vmRID, len(storages))
		for _, st := range storages {
			if st.Dataset != nil {
				log.Printf("[DEBUG] StepCreateFromTemplate cleanup: storage id=%d type=%s name=%s pool=%s dataset=%s/%s",
					st.ID, st.Type, st.Name, st.Pool, st.Dataset.Pool, st.Dataset.Name)
			} else {
				log.Printf("[DEBUG] StepCreateFromTemplate cleanup: storage id=%d type=%s name=%s pool=%s (no ZFS dataset record)",
					st.ID, st.Type, st.Name, st.Pool)
			}
		}
	}

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)
	if err := c.DeleteVM(vmRID); err != nil {
		ui.Error(fmt.Sprintf("Cleanup: failed to delete VM RID=%d: %s", vmRID, err))
	} else {
		ui.Say(fmt.Sprintf("Cleanup: VM RID=%d deleted", vmRID))
		state.Put("vm_rid", uint(0))
	}
}
