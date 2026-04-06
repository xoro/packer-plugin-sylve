// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

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
// reaches the Running state. The same step struct is embedded twice in the
// builder step sequence: once for Phase A (installer boot) and once for
// Phase B (provision boot after install).
type StepStartVM struct {
	Config *Config
}

// startVMPollInterval and startVMMaxWait are overridable in tests.
var (
	startVMPollInterval = 3 * time.Second
	startVMMaxWait      = 5 * time.Minute
)

func (s *StepStartVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packersdk.Ui)
	rid, _ := state.Get("vm_rid").(uint)

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	ui.Say(fmt.Sprintf("Starting VM rid=%d...", rid))
	if err := c.StartVM(rid); err != nil {
		err = fmt.Errorf("start VM: %w", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	timeout := startVMMaxWait
	deadline := time.Now().Add(timeout)
	var lastState client.DomainState = -1
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

		vm, err := c.GetVMByRID(rid)
		if err != nil {
			ui.Say(fmt.Sprintf("Waiting for VM rid=%d (poll error: %s)", rid, err))
			continue
		}
		if vm.State != lastState {
			log.Printf("[DEBUG] VM rid=%d state: %d", rid, vm.State)
			lastState = vm.State
		}
		if vm.State == client.DomainStateRunning || vm.State == client.DomainStateNoState {
			ui.Say(fmt.Sprintf("VM rid=%d is running", rid))
			if logs, err := c.GetVMLogs(rid); err == nil && logs != "" {
				log.Printf("[DEBUG] VM rid=%d bhyve log:\n%s", rid, logs)
			}
			return multistep.ActionContinue
		}
	}
}

func (s *StepStartVM) Cleanup(_ multistep.StateBag) {}
