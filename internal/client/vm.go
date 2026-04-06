// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"fmt"
	"time"
)

// VM represents the Sylve VM model returned by GET /api/vm/:id.
type VM struct {
	ID            uint        `json:"id"`
	RID           uint        `json:"rid"`
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	CPUSockets    int         `json:"cpuSockets"`
	CPUCores      int         `json:"cpuCores"`
	CPUThreads    int         `json:"cpuThreads"`
	RAM           int         `json:"ram"`
	VNCPort       int         `json:"vncPort"`
	VNCPassword   string      `json:"vncPassword"`
	VNCResolution string      `json:"vncResolution"`
	VNCWait       bool        `json:"vncWait"`
	State         DomainState `json:"state"`
	Networks      []VMNetwork `json:"networks"`
	Storages      []VMStorage `json:"storages"`
	ACPI          bool        `json:"acpi"`
	APIC          bool        `json:"apic"`
	StartedAt     time.Time   `json:"startedAt"`
	StoppedAt     time.Time   `json:"stoppedAt"`
}

// VMNetworkObject mirrors the macObj embedded in a VMNetwork.
type VMNetworkObject struct {
	Entries []VMNetworkObjectEntry `json:"entries"`
}

// VMNetworkObjectEntry holds a single value (e.g. a MAC address string).
type VMNetworkObjectEntry struct {
	Value string `json:"value"`
}

// VMNetwork is a network interface attached to a VM.
type VMNetwork struct {
	ID        uint             `json:"id"`
	MAC       string           `json:"mac"`
	MacObj    *VMNetworkObject `json:"macObj"`
	Emulation string           `json:"emulation"`
	VMID      uint             `json:"vmId"`
}

// MACAddress returns the MAC address for this network interface.
// Sylve stores the real MAC in MacObj.Entries[0].Value; the top-level MAC
// field is always empty in the current API version.
func (n *VMNetwork) MACAddress() string {
	if n.MAC != "" {
		return n.MAC
	}
	if n.MacObj != nil && len(n.MacObj.Entries) > 0 {
		return n.MacObj.Entries[0].Value
	}
	return ""
}

// VMStorage is a storage device attached to a VM.
type VMStorage struct {
	ID        uint   `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Emulation string `json:"emulation"`
	BootOrder int    `json:"bootOrder"`
	VMID      uint   `json:"vmId"`
}

// SimpleVM is the lightweight object returned by GET /api/vm/simple.
type SimpleVM struct {
	RID     uint        `json:"rid"`
	ID      uint        `json:"id"`
	Name    string      `json:"name"`
	State   DomainState `json:"state"`
	VNCPort int         `json:"vncPort"`
}

// CreateVMRequest is the body sent to POST /api/vm.
// NOTE: RID is a required pointer field in the Sylve API (binding:"required").
// We always send 0 and expect Sylve to auto-assign the actual RID.
// TODO: If the API returns rid_or_name_already_in_use or a validation error on
// RID, this assumption is wrong — check the full error body logged below and
// consider generating a random uint in 1..65535 as a fallback.
type CreateVMRequest struct {
	Name        string `json:"name"`
	RID         *uint  `json:"rid"`
	Description string `json:"description,omitempty"`

	ISO string `json:"iso,omitempty"`

	StoragePool          string `json:"storagePool,omitempty"`
	StorageType          string `json:"storageType,omitempty"`
	StorageSize          *int64 `json:"storageSize,omitempty"`
	StorageEmulationType string `json:"storageEmulationType,omitempty"`

	SwitchName          string `json:"switchName,omitempty"`
	SwitchEmulationType string `json:"switchEmulationType,omitempty"`

	CPUSockets int `json:"cpuSockets"`
	CPUCores   int `json:"cpuCores"`
	CPUThreads int `json:"cpuThreads"`

	RAM int `json:"ram"`

	VNCPort       int    `json:"vncPort"`
	VNCPassword   string `json:"vncPassword,omitempty"`
	VNCResolution string `json:"vncResolution"`
	VNCWait       *bool  `json:"vncWait"`

	Loader string `json:"loader,omitempty"`

	TimeOffset string `json:"timeOffset"`

	ACPI *bool `json:"acpi"`
	APIC *bool `json:"apic"`
}

// CreateVM calls POST /api/vm. On success the response body contains no VM
// data; use ListVMsSimple to find the created VM by name.
func (c *Client) CreateVM(req CreateVMRequest) error {
	var resp APIResponse[interface{}]
	if err := c.post("/vm", req, &resp); err != nil {
		return fmt.Errorf("create VM %q: %w", req.Name, err)
	}
	return nil
}

// GetVMByRID calls GET /api/vm/:rid?type=rid and returns the full VM object.
func (c *Client) GetVMByRID(rid uint) (*VM, error) {
	var resp APIResponse[VM]
	path := fmt.Sprintf("/vm/%d?type=rid", rid)
	if err := c.get(path, &resp); err != nil {
		return nil, fmt.Errorf("get VM rid=%d: %w", rid, err)
	}
	return &resp.Data, nil
}

// ListVMsSimple calls GET /api/vm/simple and returns all VMs as lightweight objects.
func (c *Client) ListVMsSimple() ([]SimpleVM, error) {
	var resp APIResponse[[]SimpleVM]
	if err := c.get("/vm/simple", &resp); err != nil {
		return nil, fmt.Errorf("list VMs simple: %w", err)
	}
	return resp.Data, nil
}

// GetSimpleVMByRID calls GET /api/vm/simple/:rid?type=rid and returns a
// lightweight VM object with live State populated from libvirt.
// Unlike GetVMByRID, which always returns State=0 because the full VM
// endpoint does not query libvirt, the simple endpoint does call
// GetDomainState and returns the real runtime state.
func (c *Client) GetSimpleVMByRID(rid uint) (*SimpleVM, error) {
	var resp APIResponse[SimpleVM]
	path := fmt.Sprintf("/vm/simple/%d?type=rid", rid)
	if err := c.get(path, &resp); err != nil {
		return nil, fmt.Errorf("get simple VM rid=%d: %w", rid, err)
	}
	return &resp.Data, nil
}

// StartVM calls POST /api/vm/start/:rid.
func (c *Client) StartVM(rid uint) error {
	path := fmt.Sprintf("/vm/start/%d", rid)
	var resp APIResponse[interface{}]
	if err := c.post(path, nil, &resp); err != nil {
		return fmt.Errorf("start VM rid=%d: %w", rid, err)
	}
	return nil
}

// StopVM calls POST /api/vm/stop/:rid.
func (c *Client) StopVM(rid uint) error {
	path := fmt.Sprintf("/vm/stop/%d", rid)
	var resp APIResponse[interface{}]
	if err := c.post(path, nil, &resp); err != nil {
		return fmt.Errorf("stop VM rid=%d: %w", rid, err)
	}
	return nil
}

// GetVMLogs calls GET /api/vm/logs/:rid and returns the last 512 lines of the bhyve log.
func (c *Client) GetVMLogs(rid uint) (string, error) {
	type logsData struct {
		Logs string `json:"logs"`
	}
	var resp APIResponse[logsData]
	path := fmt.Sprintf("/vm/logs/%d", rid)
	if err := c.get(path, &resp); err != nil {
		return "", fmt.Errorf("get VM logs rid=%d: %w", rid, err)
	}
	return resp.Data.Logs, nil
}

// StorageUpdateRequest is the body sent to PUT /api/vm/storage/update.
// For DiskImage (ISO) storages, Size may be nil.
type StorageUpdateRequest struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Emulation string `json:"emulation"`
	BootOrder *int   `json:"bootOrder,omitempty"`
	Enable    *bool  `json:"enable,omitempty"`
}

// UpdateStorageBootOrder calls PUT /api/vm/storage/update to change the boot
// order of a storage device without modifying any other property.
func (c *Client) UpdateStorageBootOrder(storageID int, name, emulation string, bootOrder int) error {
	req := StorageUpdateRequest{
		ID:        storageID,
		Name:      name,
		Emulation: emulation,
		BootOrder: &bootOrder,
	}
	var resp APIResponse[interface{}]
	if err := c.put("/vm/storage/update", req, &resp); err != nil {
		return fmt.Errorf("UpdateStorageBootOrder id=%d: %w", storageID, err)
	}
	return nil
}

// DisableISOStorage calls PUT /api/vm/storage/update to set enable=false on an
// ISO/CD storage device. This causes SyncVMDisks to omit the CD from the bhyve
// command line on the next start, forcing UEFI to boot from the zvol regardless
// of whatever BootOrder entries the UEFI NVRAM accumulated during the first boot.
func (c *Client) DisableISOStorage(storageID int, name, emulation string) error {
	enabled := false
	req := StorageUpdateRequest{
		ID:        storageID,
		Name:      name,
		Emulation: emulation,
		Enable:    &enabled,
	}
	var resp APIResponse[interface{}]
	if err := c.put("/vm/storage/update", req, &resp); err != nil {
		return fmt.Errorf("DisableISOStorage id=%d: %w", storageID, err)
	}
	return nil
}

// DisableStartAtBoot calls PUT /api/vm/options/boot-order/:rid to set
// startAtBoot=false on the VM. Sylve auto-restarts VMs with startAtBoot=true
// after every stop; disabling it ensures the plugin controls all restarts and
// prevents Sylve from firing a competing restart (with the ISO still enabled)
// right after the installer force-stop.
func (c *Client) DisableStartAtBoot(rid uint) error {
	bootOrder := 0
	falseVal := false
	req := struct {
		StartAtBoot *bool `json:"startAtBoot"`
		BootOrder   *int  `json:"bootOrder"`
	}{
		StartAtBoot: &falseVal,
		BootOrder:   &bootOrder,
	}
	var resp APIResponse[interface{}]
	path := fmt.Sprintf("/vm/options/boot-order/%d", rid)
	if err := c.put(path, req, &resp); err != nil {
		return fmt.Errorf("DisableStartAtBoot rid=%d: %w", rid, err)
	}
	return nil
}

// HasActiveLifecycleTask returns true when Sylve has an active lifecycle task
// for the given VM (by database ID). Sylve rejects StartVM with 409
// lifecycle_task_in_progress while a previous stop/start task is still running;
// polling this endpoint before calling StartVM avoids that race.
func (c *Client) HasActiveLifecycleTask(vmID uint) (bool, error) {
	var resp APIResponse[map[string]interface{}]
	path := fmt.Sprintf("/tasks/lifecycle/active/vm/%d", vmID)
	if err := c.get(path, &resp); err != nil {
		return false, fmt.Errorf("HasActiveLifecycleTask vmID=%d: %w", vmID, err)
	}
	return resp.Data != nil, nil
}

// DeleteVM calls DELETE /api/vm/:rid using the VM's runtime ID (RID).
// Sylve addresses VMs by RID for all mutating operations (start, stop, delete).
func (c *Client) DeleteVM(rid uint) error {
	path := fmt.Sprintf("/vm/%d?deletemacs=true&deleterawdisks=true&deletevolumes=true", rid)
	if err := c.delete(path); err != nil {
		return fmt.Errorf("delete VM rid=%d: %w", rid, err)
	}
	return nil
}
