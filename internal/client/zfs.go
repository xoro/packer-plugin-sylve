// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import "fmt"

// createSnapshotRequest is sent to POST /api/zfs/datasets/snapshot.
type createSnapshotRequest struct {
	GUID      string `json:"guid"`
	Name      string `json:"name"`
	Recursive bool   `json:"recursive"`
}

// rollbackSnapshotRequest is sent to POST /api/zfs/datasets/snapshot/rollback.
type rollbackSnapshotRequest struct {
	GUID              string `json:"guid"`
	DestroyMoreRecent bool   `json:"destroyMoreRecent"`
}

// datasetInfo holds the fields we need from a gzfs.Dataset response entry.
type datasetInfo struct {
	GUID string `json:"guid"`
	Name string `json:"name"`
}

// listDatasets returns all ZFS datasets of the given type (e.g. "SNAPSHOT").
func (c *Client) listDatasets(dsType string) ([]datasetInfo, error) {
	path := "/zfs/datasets?type=" + dsType
	// GET /zfs/datasets returns DatasetListResponse which mirrors
	// APIResponse[[]gzfs.Dataset]: status/message/error/data all at the
	// top level, with data being a direct array (not nested).
	var resp APIResponse[[]datasetInfo]
	if err := c.get(path, &resp); err != nil {
		return nil, fmt.Errorf("list ZFS datasets type=%s: %w", dsType, err)
	}
	return resp.Data, nil
}

// TakeDatasetSnapshot creates a ZFS snapshot of the given dataset and returns
// the created snapshot's GUID. datasetGUID is VMStorage.Dataset.GUID.
// datasetName is the ZFS path (e.g. "zroot/vms/myvm"). snapshotLabel is the
// snapshot name suffix (e.g. "packer-pre-build-1716519830").
func (c *Client) TakeDatasetSnapshot(datasetGUID, datasetName, snapshotLabel string) (string, error) {
	req := createSnapshotRequest{
		GUID:      datasetGUID,
		Name:      snapshotLabel,
		Recursive: false,
	}
	var raw APIResponse[interface{}]
	if err := c.post("/zfs/datasets/snapshot", req, &raw); err != nil {
		return "", fmt.Errorf("create snapshot %q on dataset %q: %w", snapshotLabel, datasetName, err)
	}

	// POST /zfs/datasets/snapshot returns APIResponse[any]; no snapshot GUID is
	// provided in the response body. Query the snapshot list by full ZFS name
	// to obtain the GUID needed for rollback.
	fullName := datasetName + "@" + snapshotLabel
	snapshots, err := c.listDatasets("SNAPSHOT")
	if err != nil {
		return "", fmt.Errorf("list snapshots to find %q: %w", fullName, err)
	}
	for _, ds := range snapshots {
		if ds.Name == fullName {
			return ds.GUID, nil
		}
	}
	return "", fmt.Errorf("snapshot %q not found after creation", fullName)
}

// RollbackDataset rolls back the ZFS dataset to the given snapshot.
// snapshotGUID is the GUID returned by TakeDatasetSnapshot.
// Set destroyMoreRecent to false to fail safely when newer snapshots exist.
func (c *Client) RollbackDataset(snapshotGUID string, destroyMoreRecent bool) error {
	req := rollbackSnapshotRequest{
		GUID:              snapshotGUID,
		DestroyMoreRecent: destroyMoreRecent,
	}
	var raw APIResponse[interface{}]
	if err := c.post("/zfs/datasets/snapshot/rollback", req, &raw); err != nil {
		return fmt.Errorf("rollback snapshot %q: %w", snapshotGUID, err)
	}
	return nil
}
