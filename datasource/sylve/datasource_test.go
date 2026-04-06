// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylve

import "testing"

func TestDatasource_ConfigSpec_NotNil(t *testing.T) {
	d := &Datasource{}
	if d.ConfigSpec() == nil {
		t.Error("ConfigSpec() returned nil")
	}
}

func TestDatasource_Configure_NoError(t *testing.T) {
	d := &Datasource{}
	if err := d.Configure(); err != nil {
		t.Fatalf("Configure() returned error: %v", err)
	}
}

func TestDatasource_OutputSpec_NotNil(t *testing.T) {
	d := &Datasource{}
	if d.OutputSpec() == nil {
		t.Error("OutputSpec() returned nil")
	}
}

func TestDatasource_Execute_NoError(t *testing.T) {
	d := &Datasource{}
	_, err := d.Execute()
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}
}
