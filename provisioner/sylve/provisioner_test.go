// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylve

import (
	"context"
	"io"
	"testing"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestProvisioner_ConfigSpec_NotNil(t *testing.T) {
	p := &Provisioner{}
	if p.ConfigSpec() == nil {
		t.Error("ConfigSpec() returned nil")
	}
}

func TestProvisioner_Prepare_NoError(t *testing.T) {
	p := &Provisioner{}
	if err := p.Prepare(); err != nil {
		t.Fatalf("Prepare() returned error: %v", err)
	}
}

func TestProvisioner_Provision_NoError(t *testing.T) {
	p := &Provisioner{}
	ui := &packersdk.BasicUi{Writer: io.Discard, ErrorWriter: io.Discard}
	err := p.Provision(context.Background(), ui, nil, nil)
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
}
