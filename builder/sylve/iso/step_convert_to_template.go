// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package iso

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepConvertToTemplate saves the shut-off build VM as a Sylve VM template
// named template_name, so that sylve-vm builds can start from it. It does
// nothing when template_name is empty.
//
// It runs after StepShutdown, because Sylve only converts shut-off VMs, and
// before StepDeleteVM, because the template keeps its own copy of the disks.
// On success it puts "template_id" and "template_name" into the state bag.
type StepConvertToTemplate struct {
	Config *Config
}

// templatePollInterval and templateMaxWait are overridable in tests. Copying
// the disks of a large VM takes minutes, so the wait is generous.
var (
	templatePollInterval = 5 * time.Second
	templateMaxWait      = 60 * time.Minute
)

// Run converts the VM and waits for Sylve's conversion task to end. It halts
// the build when Sylve rejects the conversion, the task fails, the wait times
// out or is cancelled, or the template cannot be found afterwards.
func (s *StepConvertToTemplate) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	name := s.Config.TemplateName
	if name == "" {
		return multistep.ActionContinue
	}
	ui := state.Get("ui").(packersdk.Ui)
	rid, _ := state.Get("vm_rid").(uint)
	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	halt := func(err error) multistep.StepAction {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Sylve answers 409 lifecycle_task_in_progress while the stop task of
	// StepShutdown is still active, so let it finish first.
	if vmID, ok := state.Get("vm_id").(uint); ok {
		err := waitForTemplate(ctx, "lifecycle tasks of VM id="+fmt.Sprint(vmID)+" to clear", func() (bool, error) {
			active, err := c.HasActiveLifecycleTask(vmID)
			return err == nil && !active, err
		})
		if err != nil {
			return halt(err)
		}
	}

	ui.Say(fmt.Sprintf("Saving VM rid=%d as template %q...", rid, name))
	taskID, err := c.ConvertVMToTemplate(rid, name)
	if err != nil {
		return halt(err)
	}

	var task *client.LifecycleTask
	err = waitForTemplate(ctx, fmt.Sprintf("conversion task id=%d", taskID), func() (bool, error) {
		t, err := c.TemplateTask(rid, taskID)
		if err != nil {
			return false, err
		}
		task = t
		return t.Status == client.LifecycleTaskSuccess || t.Status == client.LifecycleTaskFailed, nil
	})
	if err != nil {
		return halt(err)
	}
	if task.Status == client.LifecycleTaskFailed {
		return halt(fmt.Errorf("sylve failed to save VM rid=%d as template %q: %s", rid, name, task.Error))
	}

	template, err := c.FindTemplateByName(name)
	if err != nil {
		return halt(err)
	}
	state.Put("template_id", template.ID)
	state.Put("template_name", template.Name)
	ui.Say(fmt.Sprintf("VM rid=%d saved as template %q (id %d)", rid, template.Name, template.ID))
	return multistep.ActionContinue
}

// Cleanup does nothing: Sylve removes a half-made template itself when its
// conversion task fails.
func (s *StepConvertToTemplate) Cleanup(_ multistep.StateBag) {}

// waitForTemplate calls done every templatePollInterval until it reports
// true. Errors from done count as "not yet" (Sylve may be briefly unreachable)
// and the last one is reported when templateMaxWait passes. Returns ctx.Err()
// when the build is cancelled.
func waitForTemplate(ctx context.Context, what string, done func() (bool, error)) error {
	deadline := time.Now().Add(templateMaxWait)
	var lastErr error
	for {
		ok, err := done()
		if ok {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("waiting for %s: no result within %s (last error: %v)", what, templateMaxWait, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(templatePollInterval):
		}
	}
}
