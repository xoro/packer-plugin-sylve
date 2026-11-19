// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package client

import "fmt"

// SimpleTemplate is the lightweight object returned by GET /api/vm/templates.
type SimpleTemplate struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	SourceVMName string `json:"sourceVmName"`
}

// CreateFromTemplateRequest is the body sent to POST /api/vm/templates/:id/vms.
// Sylve defaults the omitted mode to "single" and clones each disk into the
// pool the template was captured from when no storagePools mapping is sent.
type CreateFromTemplateRequest struct {
	Name string `json:"name"`
	RID  uint   `json:"rid"`
}

// ListTemplatesSimple calls GET /api/vm/templates and returns all templates as
// lightweight objects.
func (c *Client) ListTemplatesSimple() ([]SimpleTemplate, error) {
	var resp APIResponse[[]SimpleTemplate]
	if err := c.get("/vm/templates", &resp); err != nil {
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

// CreateVMFromTemplate calls POST /api/vm/templates/:id/vms to create a new VM
// from the given template. The request specifies the VM name and the RID to
// assign to the new VM. Sylve queues the clone as a lifecycle task (HTTP 202),
// so the VM appears only once that task has run; callers wait for it.
func (c *Client) CreateVMFromTemplate(templateID uint, req CreateFromTemplateRequest) error {
	path := fmt.Sprintf("/vm/templates/%d/vms", templateID)
	var resp APIResponse[interface{}]
	if err := c.post(path, req, &resp); err != nil {
		return fmt.Errorf("create VM from template id=%d: %w", templateID, err)
	}
	return nil
}

// ConvertToTemplateRequest is the body sent to POST /api/vm/:rid/templates.
type ConvertToTemplateRequest struct {
	Name string `json:"name"`
}

// templateCaptureTask is the data of Sylve's HTTP 202 answer to
// POST /api/vm/:rid/templates: the lifecycle task that performs the conversion.
type templateCaptureTask struct {
	TaskID uint `json:"taskId"`
}

// LifecycleTask is one Sylve guest lifecycle task as listed by
// GET /api/tasks/lifecycle/recent.
type LifecycleTask struct {
	ID     uint   `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// Lifecycle task states that end a task; "queued" and "running" do not.
const (
	LifecycleTaskSuccess = "success"
	LifecycleTaskFailed  = "failed"
)

// ConvertVMToTemplate calls POST /api/vm/:rid/templates to save the VM with
// the given RID as a VM template called name. Sylve requires the VM to be shut
// off and the name to be unique, and queues the conversion as a lifecycle task.
// Returns the ID of that task, which TemplateTask reports on; returns an error
// when Sylve rejects the request (e.g. the VM is running or the name is taken).
func (c *Client) ConvertVMToTemplate(rid uint, name string) (uint, error) {
	var resp APIResponse[templateCaptureTask]
	path := fmt.Sprintf("/vm/%d/templates", rid)
	if err := c.post(path, ConvertToTemplateRequest{Name: name}, &resp); err != nil {
		return 0, fmt.Errorf("convert VM rid=%d to template %q: %w", rid, name, err)
	}
	return resp.Data.TaskID, nil
}

// TemplateTask returns the VM template lifecycle task with ID taskID of the
// guest guestID (the source VM's RID for a conversion) from
// GET /api/tasks/lifecycle/recent. Sylve 0.3.1 has no endpoint for a single
// task. Returns an error when the request fails or the task is not listed.
func (c *Client) TemplateTask(guestID, taskID uint) (*LifecycleTask, error) {
	var resp APIResponse[[]LifecycleTask]
	path := fmt.Sprintf("/tasks/lifecycle/recent?guestType=vm-template&guestId=%d&limit=200", guestID)
	if err := c.get(path, &resp); err != nil {
		return nil, fmt.Errorf("get template task id=%d: %w", taskID, err)
	}
	for i := range resp.Data {
		if resp.Data[i].ID == taskID {
			return &resp.Data[i], nil
		}
	}
	return nil, fmt.Errorf("get template task id=%d: task not found", taskID)
}

// FreeRIDs returns the RIDs in the range 1-9999 that no VM uses, ascending.
// Sylve also refuses a jail's ID, which this list does not know: a caller
// retries on rid_range_contains_used_values. Returns an error when no RID is free.
func (c *Client) FreeRIDs() ([]uint, error) {
	vms, err := c.ListVMsSimple()
	if err != nil {
		return nil, fmt.Errorf("find free RID: %w", err)
	}
	used := make(map[uint]bool, len(vms))
	for _, vm := range vms {
		used[vm.RID] = true
	}
	var free []uint
	for rid := uint(1); rid <= 9999; rid++ {
		if !used[rid] {
			free = append(free, rid)
		}
	}
	if len(free) == 0 {
		return nil, fmt.Errorf("find free RID: all RIDs 1-9999 are in use")
	}
	return free, nil
}
