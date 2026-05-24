// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package common provides shared types and steps used by all Sylve builders.
package common

import (
	"fmt"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// Artifact is the result of a Sylve build. Each builder passes its own
// BuilderID constant so post-processors can identify compatible artifacts.
type Artifact struct {
	// BuilderID identifies the builder that produced this artifact.
	BuilderID string

	VMRID     uint
	VMID      uint
	StateData map[string]interface{}
}

// BuilderId returns the unique builder identifier for this artifact type.
func (a *Artifact) BuilderId() string {
	return a.BuilderID
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

// Destroy is a no-op: VM lifecycle management is handled by the builder.
func (a *Artifact) Destroy() error {
	return nil
}

var _ packersdk.Artifact = (*Artifact)(nil)
