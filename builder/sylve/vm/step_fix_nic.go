// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepFixNIC works around a Sylve bug where template-cloned VMs have their
// NIC record set to enable=false. When enable=false, CreateVmXML omits the
// virtio-net device from the bhyve command line, so the guest starts without
// network connectivity.
//
// Sylve's network/detach and network/attach APIs require the libvirt domain to
// exist (they call check_vm_inactive which looks up the domain by name). After
// a fresh template clone, no domain exists yet. The step therefore performs a
// bootstrap start to create the domain before fixing the NIC:
//
//  1. Start the VM (creates the libvirt domain; bhyve may crash or stay stuck
//     because the NIC is missing — this is expected).
//  2. Wait until the domain leaves NoState (any state > 0), confirming it exists.
//  3. Stop the VM so the domain is inactive.
//  4. Detach the stale NIC record (the one with enable=false).
//  5. Re-attach a new NIC via NetworkAttach, which writes the virtio-net device
//     into the stored domain XML.
//
// StepStartVM (which follows this step) performs the "real" start that expects
// the VM to reach Running state with a working NIC.
//
// MAC address handling depends on the communicator:
//   - WinRM (Windows): preserves the original macId so the MAC stays the same.
//     Windows identifies adapters by MAC; a new MAC loses WinRM/firewall config.
//   - SSH (Unix): uses macId=nil so Sylve generates a fresh random MAC,
//     avoiding collisions with the template's original MAC.
type StepFixNIC struct {
	Config *Config
}

// fixNICPollInterval controls polling during bootstrap start/stop.
// fixNICBootstrapMaxWait is the maximum time to wait for the domain to be created.
var (
	fixNICPollInterval     = 3 * time.Second
	fixNICBootstrapMaxWait = 2 * time.Minute
	fixNICStopMaxWait      = 2 * time.Minute
)

func (s *StepFixNIC) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	rid, _ := state.Get("vm_rid").(uint)
	netID, _ := state.Get("vm_network_id").(uint)

	if netID == 0 {
		log.Printf("[DEBUG] step_fix_nic: no vm_network_id in state — skipping NIC fix")
		return multistep.ActionContinue
	}

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	// Resolve switch name: API response field → SYLVE_SWITCH env → ListSwitches.
	switchName, _ := state.Get("vm_network_switch").(string)
	emulation, _ := state.Get("vm_network_emulation").(string)
	if emulation == "" {
		emulation = "virtio-net"
	}

	if switchName == "" {
		switchName = os.Getenv("SYLVE_SWITCH")
		if switchName != "" {
			log.Printf("[DEBUG] step_fix_nic: resolved switch from SYLVE_SWITCH env: %q", switchName)
		}
	}
	if switchName == "" {
		if switches, err := c.ListSwitches(); err == nil && len(switches.Standard) > 0 {
			switchName = switches.Standard[0].Name
			log.Printf("[DEBUG] step_fix_nic: resolved switch from ListSwitches: %q", switchName)
		} else if err != nil {
			log.Printf("[DEBUG] step_fix_nic: ListSwitches error: %s", err)
		}
	}
	if switchName == "" {
		ui.Error("Cannot fix NIC: no switch name available (set SYLVE_SWITCH or configure a switch in Sylve)")
		err := fmt.Errorf("NIC fix failed: no switch name")
		state.Put("error", err)
		return multistep.ActionHalt
	}

	// 1. Bootstrap start: create the libvirt domain. The VM will likely crash
	// or stay stuck because the NIC is disabled — that is expected.
	ui.Say(fmt.Sprintf("Fixing NIC: bootstrap-starting VM rid=%d to create domain...", rid))
	if err := c.StartVM(rid); err != nil {
		ui.Error(fmt.Sprintf("NIC fix: bootstrap start rid=%d: %s", rid, err))
		state.Put("error", fmt.Errorf("NIC fix bootstrap start: %w", err))
		return multistep.ActionHalt
	}

	// 2. Wait until the domain transitions out of NoState (state > 0).
	// Any non-zero state confirms the libvirt domain was created.
	bootstrapDeadline := time.Now().Add(fixNICBootstrapMaxWait)
	domainCreated := false
	for {
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(fixNICPollInterval):
		}
		vm, err := c.GetSimpleVMByRID(rid)
		if err != nil {
			log.Printf("[DEBUG] step_fix_nic: bootstrap poll error: %s", err)
			continue
		}
		if vm.State != client.DomainStateNoState {
			log.Printf("[DEBUG] step_fix_nic: domain created (state=%d)", vm.State)
			domainCreated = true
			break
		}
		if time.Now().After(bootstrapDeadline) {
			break
		}
	}
	if !domainCreated {
		// Domain never left NoState. The detach/attach will likely fail, but
		// try anyway — Sylve may have created the domain without updating state.
		log.Printf("[DEBUG] step_fix_nic: domain still NoState after bootstrap; attempting stop+fix anyway")
	}

	// 3. Stop the VM so the domain is inactive (required for detach/attach).
	ui.Say("Stopping VM after bootstrap start...")
	if err := c.StopVM(rid); err != nil {
		log.Printf("[DEBUG] step_fix_nic: stop after bootstrap: %s (may already be stopped)", err)
	}

	// Poll until VM is not Running/Blocked.
	stopDeadline := time.Now().Add(fixNICStopMaxWait)
	for {
		select {
		case <-ctx.Done():
			state.Put("error", ctx.Err())
			return multistep.ActionHalt
		case <-time.After(fixNICPollInterval):
		}
		vm, err := c.GetSimpleVMByRID(rid)
		if err != nil {
			log.Printf("[DEBUG] step_fix_nic: stop-poll error: %s", err)
			continue
		}
		if vm.State == client.DomainStateShutoff || vm.State == client.DomainStateCrashed ||
			vm.State == client.DomainStateNoState || vm.State == client.DomainStateShutdown {
			log.Printf("[DEBUG] step_fix_nic: VM stopped (state=%d)", vm.State)
			break
		}
		if time.Now().After(stopDeadline) {
			err := fmt.Errorf("NIC fix: VM rid=%d did not stop within %s", rid, fixNICStopMaxWait)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
	}

	// 4. Detach the stale NIC record (enable=false).
	ui.Say(fmt.Sprintf("Detaching stale NIC (id=%d)...", netID))
	if err := c.DetachVMNetwork(rid, netID); err != nil {
		ui.Error(fmt.Sprintf("NIC fix: detach id=%d: %s", netID, err))
		state.Put("error", fmt.Errorf("NIC fix detach: %w", err))
		return multistep.ActionHalt
	}

	// 5. Re-attach NIC. For WinRM (Windows) preserve the original MAC so the
	// guest recognises the same adapter and WinRM/firewall config is retained.
	// For SSH (Unix) use a fresh MAC to avoid collisions with the template.
	var macIDPtr *uint
	if s.Config.Config.Type == "winrm" {
		if origMacID, ok := state.Get("vm_network_mac_id").(uint); ok && origMacID != 0 {
			macIDPtr = &origMacID
			log.Printf("[DEBUG] step_fix_nic: WinRM communicator — preserving original macId=%d", origMacID)
		}
	}

	ui.Say(fmt.Sprintf("Attaching NIC (switch=%q, emulation=%q)...", switchName, emulation))
	if err := c.ReattachVMNetwork(rid, switchName, emulation, macIDPtr); err != nil {
		ui.Error(fmt.Sprintf("NIC fix: reattach rid=%d: %s", rid, err))
		state.Put("error", fmt.Errorf("NIC fix reattach: %w", err))
		return multistep.ActionHalt
	}

	// Re-fetch VM to get the new MAC for StepDiscoverIP.
	if updated, err := c.GetVMByRID(rid); err == nil && len(updated.Networks) > 0 {
		mac := updated.Networks[0].MACAddress()
		state.Put("vm_mac", mac)
		ui.Say(fmt.Sprintf("NIC fixed: MAC %s", mac))
	} else {
		log.Printf("[DEBUG] step_fix_nic: re-fetch VM after reattach: %v", err)
	}

	return multistep.ActionContinue
}

func (s *StepFixNIC) Cleanup(_ multistep.StateBag) {}
