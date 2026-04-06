// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"fmt"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// BuilderID is the unique identifier for artifacts produced by the sylve-iso
// builder. Post-processors use this value to filter for compatible artifacts.
const BuilderID = "xoro.sylveiso"

// Artifact is the result of a sylve-iso build.
type Artifact struct {
	VMRID     uint
	VMID      uint
	StateData map[string]interface{}
}

// BuilderId returns the unique builder identifier for this artifact type.
func (a *Artifact) BuilderId() string {
	return BuilderID
}

// Files returns the list of files that make up this artifact.
// Sylve VM artifacts have no local files; the VM resides on the Sylve host.
func (a *Artifact) Files() []string {
	return nil
}

// Id returns the VM's libvirt runtime ID (RID) as a string.
func (a *Artifact) Id() string {
	return fmt.Sprintf("%d", a.VMRID)
}

// String returns a human-readable description of the artifact.
func (a *Artifact) String() string {
	return fmt.Sprintf("Sylve VM RID %d (ID %d)", a.VMRID, a.VMID)
}

// State returns a named piece of state data associated with the artifact.
func (a *Artifact) State(name string) interface{} {
	return a.StateData[name]
}

// Destroy is a no-op: VM removal is handled by the builder (after success when
// destroy=true, or by StepCreateVM.Cleanup on failure unless keep_on_error).
func (a *Artifact) Destroy() error {
	return nil
}

var _ packersdk.Artifact = (*Artifact)(nil)
