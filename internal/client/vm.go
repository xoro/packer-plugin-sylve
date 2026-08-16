// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"fmt"
	"log"
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
	ID         uint             `json:"id"`
	MacID      *uint            `json:"macId"`
	MAC        string           `json:"mac"`
	MacObj     *VMNetworkObject `json:"macObj"`
	Emulation  string           `json:"emulation"`
	SwitchName string           `json:"switchName"`
	VMID       uint             `json:"vmId"`
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

// VMStorageTypeZVol and its siblings are the values the Sylve API returns for
// VMStorage.Type. Use these constants rather than bare strings when branching
// on storage type so refactoring catches all call sites.
const (
	VMStorageTypeRaw        = "raw"
	VMStorageTypeZVol       = "zvol"
	VMStorageTypeDiskImage  = "image"
	VMStorageTypeFilesystem = "filesystem"
)

// VMStorageDataset is the ZFS dataset record embedded in a VMStorage entry.
// It is populated by Sylve for zvol and filesystem storage types.
type VMStorageDataset struct {
	GUID string `json:"guid"`
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Pool string `json:"pool"`
}

// VMStorage is a storage device attached to a VM.
type VMStorage struct {
	ID        uint              `json:"id"`
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	Pool      string            `json:"pool"`
	Emulation string            `json:"emulation"`
	BootOrder int               `json:"bootOrder"`
	VMID      uint              `json:"vmId"`
	Dataset   *VMStorageDataset `json:"dataset"`
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

// FindVMByName iterates the simple VM list and returns the full VM for the
// first entry whose Name matches name (case-sensitive). Returns an error
// wrapping the literal "VM not found" message when no match is found.
func (c *Client) FindVMByName(name string) (*VM, error) {
	vms, err := c.ListVMsSimple()
	if err != nil {
		return nil, fmt.Errorf("find VM %q: %w", name, err)
	}
	for _, v := range vms {
		if v.Name == name {
			return c.GetVMByRID(v.RID)
		}
	}
	return nil, fmt.Errorf("find VM %q: VM not found", name)
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

// performVMAction calls POST /api/vm/:rid/actions/:action (start, stop,
// shutdown, or reboot). Sylve routes all VM lifecycle actions through this
// single endpoint as of the REST-semantics refactor; the older dedicated
// /vm/start/:rid and /vm/stop/:rid endpoints no longer exist.
func (c *Client) performVMAction(rid uint, action string) error {
	path := fmt.Sprintf("/vm/%d/actions/%s", rid, action)
	var resp APIResponse[interface{}]
	if err := c.post(path, nil, &resp); err != nil {
		return fmt.Errorf("%s VM rid=%d: %w", action, rid, err)
	}
	return nil
}

// StartVM calls POST /api/vm/:rid/actions/start.
func (c *Client) StartVM(rid uint) error {
	return c.performVMAction(rid, "start")
}

// StopVM calls POST /api/vm/:rid/actions/stop.
func (c *Client) StopVM(rid uint) error {
	return c.performVMAction(rid, "stop")
}

// GetVMLogs calls GET /api/vm/:rid/logs and returns the last 512 lines of the bhyve log.
func (c *Client) GetVMLogs(rid uint) (string, error) {
	type logsData struct {
		Logs string `json:"logs"`
	}
	var resp APIResponse[logsData]
	path := fmt.Sprintf("/vm/%d/logs", rid)
	if err := c.get(path, &resp); err != nil {
		return "", fmt.Errorf("get VM logs rid=%d: %w", rid, err)
	}
	return resp.Data.Logs, nil
}

// StorageUpdateRequest is the body sent to PATCH /api/vm/:rid/storage/:storageId.
// For DiskImage (ISO) storages, Size may be nil. RID and storage ID are now
// path parameters, not body fields.
type StorageUpdateRequest struct {
	BootOrder *int  `json:"bootOrder,omitempty"`
	Enable    *bool `json:"enable,omitempty"`
}

// UpdateStorageBootOrder calls PATCH /api/vm/:rid/storage/:storageId to change
// the boot order of a storage device without modifying any other property.
func (c *Client) UpdateStorageBootOrder(rid uint, storageID int, bootOrder int) error {
	req := StorageUpdateRequest{BootOrder: &bootOrder}
	var resp APIResponse[interface{}]
	path := fmt.Sprintf("/vm/%d/storage/%d", rid, storageID)
	if err := c.patch(path, req, &resp); err != nil {
		return fmt.Errorf("UpdateStorageBootOrder rid=%d id=%d: %w", rid, storageID, err)
	}
	return nil
}

// DetachVMNetwork calls DELETE /api/vm/:rid/networks/:networkId to remove a
// NIC database record from the VM. When the NIC was created with
// enable=false it is never added to the stored libvirt XML; Sylve's
// NetworkDetach handler detects this and deletes the database record only
// (without touching the XML), returning success. This makes it safe to call
// for any network record, including those that were never written to the
// stored XML.
func (c *Client) DetachVMNetwork(rid, networkID uint) error {
	var resp APIResponse[interface{}]
	path := fmt.Sprintf("/vm/%d/networks/%d", rid, networkID)
	if err := c.deleteWithResponse(path, &resp); err != nil {
		return fmt.Errorf("DetachVMNetwork rid=%d networkID=%d: %w", rid, networkID, err)
	}
	return nil
}

// NetworkAttachRequest is the body sent to POST /api/vm/:rid/networks.
type NetworkAttachRequest struct {
	SwitchName string `json:"switchName"`
	Emulation  string `json:"emulation"`
	MacID      *uint  `json:"macId,omitempty"`
}

// ReattachVMNetwork calls POST /api/vm/:rid/networks to add a fresh NIC
// record to the VM and write the NIC element into the stored libvirt XML.
// Unlike PATCH /vm/:rid/networks/:networkId, NetworkAttach unconditionally
// writes the NIC into the stored domain XML regardless of the enable flag.
// The next DomainCreate (vm start) therefore includes the virtio-net device
// in the bhyve command line and the guest receives a DHCP lease as expected.
//
// macObjectID may be nil; if omitted Sylve generates a new random MAC address.
// Pass the original MAC object ID to preserve the MAC address that was
// resolved into vm_mac during VM creation, so StepDiscoverIP finds the correct
// DHCP lease.
func (c *Client) ReattachVMNetwork(rid uint, switchName, emulation string, macObjectID *uint) error {
	req := NetworkAttachRequest{
		SwitchName: switchName,
		Emulation:  emulation,
		MacID:      macObjectID,
	}
	var resp APIResponse[interface{}]
	path := fmt.Sprintf("/vm/%d/networks", rid)
	if err := c.post(path, req, &resp); err != nil {
		return fmt.Errorf("ReattachVMNetwork rid=%d switch=%q: %w", rid, switchName, err)
	}
	return nil
}

// DisableISOStorage calls PATCH /api/vm/:rid/storage/:storageId to set
// enable=false on an ISO/CD storage device. This causes SyncVMDisks to omit
// the CD from the bhyve command line on the next start, forcing UEFI to boot
// from the zvol regardless of whatever BootOrder entries the UEFI NVRAM
// accumulated during the first boot.
func (c *Client) DisableISOStorage(rid uint, storageID int) error {
	enabled := false
	req := StorageUpdateRequest{Enable: &enabled}
	var resp APIResponse[interface{}]
	path := fmt.Sprintf("/vm/%d/storage/%d", rid, storageID)
	if err := c.patch(path, req, &resp); err != nil {
		return fmt.Errorf("DisableISOStorage rid=%d id=%d: %w", rid, storageID, err)
	}
	return nil
}

// DisableStartAtBoot calls PUT /api/vm/:rid/options/boot-order to set
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
	path := fmt.Sprintf("/vm/%d/options/boot-order", rid)
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
// force=false ensures Sylve performs a graceful shutdown before deletion.
func (c *Client) DeleteVM(rid uint) error {
	path := fmt.Sprintf("/vm/%d?deletemacs=true&deleterawdisks=true&deletevolumes=true&force=false", rid)
	log.Printf("[DEBUG] DeleteVM: rid=%d sending DELETE /api%s", rid, path)
	if err := c.delete(path); err != nil {
		return fmt.Errorf("delete VM rid=%d: %w", rid, err)
	}
	log.Printf("[DEBUG] DeleteVM: rid=%d delete accepted by Sylve", rid)
	return nil
}
