// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylvevm

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// StepSnapshotDisks creates a ZFS snapshot of every zvol/filesystem-backed
// storage dataset attached to the VM when preserve_original is true.
//
// The snapshot GUID map is stored in state["snapshot_guids"] as
// map[string]string (datasetGUID → snapshotGUID). Cleanup rolls back all
// datasets to their pre-build snapshot so the VM is restored regardless of
// whether the build succeeded or failed.
//
// Only zvol and filesystem storage types are snapshotted; raw (image) and other
// types do not have a ZFS dataset and are skipped.
type StepSnapshotDisks struct {
	Config *Config
}

func (s *StepSnapshotDisks) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	if !s.Config.PreserveOriginal {
		return multistep.ActionContinue
	}

	ui := state.Get("ui").(packersdk.Ui)
	storages, _ := state.Get("vm_storages").([]client.VMStorage)

	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)
	label := fmt.Sprintf("packer-pre-build-%d", time.Now().Unix())

	// snapshotGUIDs maps datasetGUID → snapshotGUID for rollback in Cleanup.
	snapshotGUIDs := make(map[string]string)

	for _, stor := range storages {
		if stor.Type != client.VMStorageTypeZVol && stor.Type != client.VMStorageTypeFilesystem {
			log.Printf("[DEBUG] step_snapshot_disks: skip storage %q type=%q", stor.Name, stor.Type)
			continue
		}
		if stor.Dataset == nil {
			log.Printf("[DEBUG] step_snapshot_disks: storage %q has no dataset; skipping", stor.Name)
			continue
		}

		ui.Say(fmt.Sprintf("Snapshotting dataset %q (@%s)...", stor.Dataset.Name, label))
		snapshotGUID, err := c.TakeDatasetSnapshot(stor.Dataset.GUID, stor.Dataset.Name, label)
		if err != nil {
			err = fmt.Errorf("snapshot dataset %q: %w", stor.Dataset.Name, err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		snapshotGUIDs[stor.Dataset.GUID] = snapshotGUID
		log.Printf("[DEBUG] step_snapshot_disks: snapshot %q guid=%s", stor.Dataset.Name, snapshotGUID)
	}

	state.Put("snapshot_guids", snapshotGUIDs)
	if len(snapshotGUIDs) > 0 {
		ui.Say(fmt.Sprintf("Created %d disk snapshot(s) for rollback", len(snapshotGUIDs)))
	}
	return multistep.ActionContinue
}

// Cleanup rolls back all snapshotted datasets to their pre-build state. It runs
// regardless of whether the build succeeded or failed — both paths restore the
// original VM disk contents when preserve_original is true.
func (s *StepSnapshotDisks) Cleanup(state multistep.StateBag) {
	if !s.Config.PreserveOriginal {
		return
	}

	snapshotGUIDs, ok := state.Get("snapshot_guids").(map[string]string)
	if !ok || len(snapshotGUIDs) == 0 {
		return
	}

	ui := state.Get("ui").(packersdk.Ui)
	c := client.New(s.Config.SylveURL, s.Config.SylveToken, s.Config.TLSSkipVerify)

	for _, snapshotGUID := range snapshotGUIDs {
		ui.Say(fmt.Sprintf("Rolling back snapshot %s...", snapshotGUID))
		if err := c.RollbackDataset(snapshotGUID, false); err != nil {
			// Log but do not halt — attempt all rollbacks even if one fails.
			log.Printf("[ERROR] step_snapshot_disks: rollback snapshot %s: %s", snapshotGUID, err)
			ui.Error(fmt.Sprintf("Rollback snapshot %s failed: %s", snapshotGUID, err))
		}
	}
}
