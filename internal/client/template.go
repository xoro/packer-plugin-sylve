// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import "fmt"

// SimpleTemplate is the lightweight object returned by GET /api/vm/templates/simple.
type SimpleTemplate struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	SourceVMName string `json:"sourceVmName"`
}

// CreateFromTemplateRequest is the body sent to POST /api/vm/templates/create/:id.
type CreateFromTemplateRequest struct {
	Name string `json:"name"`
	RID  uint   `json:"rid"`
}

// ListTemplatesSimple calls GET /api/vm/templates/simple and returns all
// templates as lightweight objects.
func (c *Client) ListTemplatesSimple() ([]SimpleTemplate, error) {
	var resp APIResponse[[]SimpleTemplate]
	if err := c.get("/vm/templates/simple", &resp); err != nil {
		return nil, fmt.Errorf("list templates simple: %w", err)
	}
	return resp.Data, nil
}

// FindTemplateByName iterates the simple template list and returns the first
// entry whose Name matches name (case-sensitive). Returns an error when no
// match is found.
func (c *Client) FindTemplateByName(name string) (*SimpleTemplate, error) {
	templates, err := c.ListTemplatesSimple()
	if err != nil {
		return nil, fmt.Errorf("find template %q: %w", name, err)
	}
	for i := range templates {
		if templates[i].Name == name {
			return &templates[i], nil
		}
	}
	return nil, fmt.Errorf("find template %q: template not found", name)
}

// CreateVMFromTemplate calls POST /api/vm/templates/create/:id to create a
// new VM from the given template. The request specifies the VM name and the
// RID to assign to the new VM.
func (c *Client) CreateVMFromTemplate(templateID uint, req CreateFromTemplateRequest) error {
	path := fmt.Sprintf("/vm/templates/create/%d", templateID)
	var resp APIResponse[interface{}]
	if err := c.post(path, req, &resp); err != nil {
		return fmt.Errorf("create VM from template id=%d: %w", templateID, err)
	}
	return nil
}

// FindNextFreeRID scans the existing VMs and returns the first RID in the
// range 1-9999 that is not in use. Returns an error when no free RID exists.
func (c *Client) FindNextFreeRID() (uint, error) {
	vms, err := c.ListVMsSimple()
	if err != nil {
		return 0, fmt.Errorf("find free RID: %w", err)
	}
	used := make(map[uint]bool, len(vms))
	for _, vm := range vms {
		used[vm.RID] = true
	}
	for rid := uint(1); rid <= 9999; rid++ {
		if !used[rid] {
			return rid, nil
		}
	}
	return 0, fmt.Errorf("find free RID: all RIDs 1-9999 are in use")
}
