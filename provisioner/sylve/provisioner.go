// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package sylve provides the Sylve Packer provisioner stub.
// Full implementation planned for a future release.
package sylve

import (
	"context"

	"github.com/hashicorp/hcl/v2/hcldec"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/zclconf/go-cty/cty"
)

// Provisioner is a stub implementation.
type Provisioner struct{}

func (p *Provisioner) ConfigSpec() hcldec.ObjectSpec {
	return hcldec.ObjectSpec{
		"script": &hcldec.AttrSpec{Name: "script", Type: cty.String, Required: false},
	}
}

func (p *Provisioner) Prepare(_ ...interface{}) error {
	return nil
}

func (p *Provisioner) Provision(_ context.Context, ui packersdk.Ui, _ packersdk.Communicator, _ map[string]interface{}) error {
	ui.Error("sylve provisioner is not yet implemented")
	return nil
}
