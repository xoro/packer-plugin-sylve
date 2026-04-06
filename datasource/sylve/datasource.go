// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package sylve provides the Sylve Packer datasource stub.
// Full implementation planned for a future release.
package sylve

import (
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/zclconf/go-cty/cty"
)

// Datasource is a stub implementation.
type Datasource struct{}

func (d *Datasource) ConfigSpec() hcldec.ObjectSpec {
	return hcldec.ObjectSpec{
		"sylve_url": &hcldec.AttrSpec{Name: "sylve_url", Type: cty.String, Required: false},
	}
}

func (d *Datasource) Configure(_ ...interface{}) error {
	return nil
}

func (d *Datasource) OutputSpec() hcldec.ObjectSpec {
	return hcldec.ObjectSpec{}
}

func (d *Datasource) Execute() (cty.Value, error) {
	return cty.NilVal, nil
}
