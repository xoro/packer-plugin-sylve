// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package sylve provides the Sylve Packer post-processor stub.
// Full implementation planned for a future release.
package sylve

import (
	"context"

	"github.com/hashicorp/hcl/v2/hcldec"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/zclconf/go-cty/cty"
)

// PostProcessor is a stub implementation.
type PostProcessor struct{}

func (p *PostProcessor) ConfigSpec() hcldec.ObjectSpec {
	return hcldec.ObjectSpec{
		"keep_input_artifact": &hcldec.AttrSpec{Name: "keep_input_artifact", Type: cty.Bool, Required: false},
	}
}

func (p *PostProcessor) Configure(_ ...interface{}) error {
	return nil
}

func (p *PostProcessor) PostProcess(_ context.Context, ui packersdk.Ui, artifact packersdk.Artifact) (packersdk.Artifact, bool, bool, error) {
	ui.Error("sylve post-processor is not yet implemented")
	return artifact, true, false, nil
}
