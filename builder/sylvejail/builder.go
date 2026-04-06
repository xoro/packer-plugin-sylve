// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package sylvejail provides the "sylve-jail" Packer builder stub.
// Creates FreeBSD Jail images via the Sylve API.
// Full implementation planned for a future release.
package sylvejail

import (
	"context"

	"github.com/hashicorp/hcl/v2/hcldec"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/zclconf/go-cty/cty"
)

// BuilderID is the unique identifier for the sylve-jail builder artifact.
const BuilderID = "xoro.sylvejail"

// Builder is a stub. Jail builder to be implemented in a future release.
type Builder struct{}

func (b *Builder) ConfigSpec() hcldec.ObjectSpec {
	return hcldec.ObjectSpec{
		"sylve_url":   &hcldec.AttrSpec{Name: "sylve_url", Type: cty.String, Required: false},
		"sylve_token": &hcldec.AttrSpec{Name: "sylve_token", Type: cty.String, Required: true},
	}
}

func (b *Builder) Prepare(_ ...interface{}) ([]string, []string, error) {
	return nil, nil, nil
}

func (b *Builder) Run(_ context.Context, ui packersdk.Ui, _ packersdk.Hook) (packersdk.Artifact, error) {
	ui.Error("sylve-jail builder is not yet implemented")
	return nil, nil
}
