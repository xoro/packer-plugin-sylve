// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import (
	"fmt"
	"strings"
)

// BasicSettings mirrors the basicSettings field returned by POST /api/auth/login.
type BasicSettings struct {
	Initialized bool     `json:"initialized"`
	Pools       []string `json:"pools"`
	Services    []string `json:"services"`
}

// GetBasicSettings calls GET /api/basic/settings.
func (c *Client) GetBasicSettings() (*BasicSettings, error) {
	var resp APIResponse[BasicSettings]
	if err := c.get("/basic/settings", &resp); err != nil {
		return nil, fmt.Errorf("get basic settings: %w", err)
	}
	return &resp.Data, nil
}

// StandardSwitch mirrors the standard switch returned by GET /api/network/switch.
type StandardSwitch struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// ManualSwitch mirrors the manual switch returned by GET /api/network/switch.
type ManualSwitch struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// SwitchList is the data payload from GET /api/network/switch.
type SwitchList struct {
	Standard []StandardSwitch `json:"standard"`
	Manual   []ManualSwitch   `json:"manual"`
}

// ListSwitches calls GET /api/network/switch and returns all switches.
func (c *Client) ListSwitches() (*SwitchList, error) {
	var resp APIResponse[SwitchList]
	if err := c.get("/network/switch", &resp); err != nil {
		return nil, fmt.Errorf("list switches: %w", err)
	}
	return &resp.Data, nil
}

// CreateStandardSwitchRequest is the body sent to POST /api/network/switch.
type CreateStandardSwitchRequest struct {
	Name    string   `json:"name"`
	Ports   []string `json:"ports"`
	Private bool     `json:"private"`
	DHCP    bool     `json:"dhcp"`
}

// CreateStandardSwitch creates a new standard switch and returns its name.
func (c *Client) CreateStandardSwitch(name string) error {
	req := CreateStandardSwitchRequest{
		Name:    name,
		Ports:   []string{},
		Private: false,
		DHCP:    true,
	}
	var resp APIResponse[interface{}]
	if err := c.post("/network/switch/standard", req, &resp); err != nil {
		return fmt.Errorf("create standard switch %q: %w", name, err)
	}
	return nil
}

// FileLease mirrors networkServiceInterfaces.FileLeases.
type FileLease struct {
	Expiry   uint64 `json:"expiry"`
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}

// Leases mirrors the response from GET /api/network/dhcp/lease.
type Leases struct {
	File []FileLease `json:"file"`
}

// GetDHCPLeases calls GET /api/network/dhcp/lease.
func (c *Client) GetDHCPLeases() (*Leases, error) {
	var resp APIResponse[Leases]
	if err := c.get("/network/dhcp/lease", &resp); err != nil {
		return nil, fmt.Errorf("get DHCP leases: %w", err)
	}
	return &resp.Data, nil
}

// FindLeaseByMAC returns the first DHCP lease whose MAC matches (case-insensitive),
// or nil if no matching lease is found yet.
func (c *Client) FindLeaseByMAC(mac string) (*FileLease, error) {
	leases, err := c.GetDHCPLeases()
	if err != nil {
		return nil, err
	}
	normalized := strings.ToLower(mac)
	for i := range leases.File {
		if strings.ToLower(leases.File[i].MAC) == normalized {
			return &leases.File[i], nil
		}
	}
	return nil, nil
}
