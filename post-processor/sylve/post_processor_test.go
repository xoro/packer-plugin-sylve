// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylve

import (
	"context"
	"io"
	"testing"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestPostProcessor_ConfigSpec_NotNil(t *testing.T) {
	p := &PostProcessor{}
	if p.ConfigSpec() == nil {
		t.Error("ConfigSpec() returned nil")
	}
}

func TestPostProcessor_Configure_NoError(t *testing.T) {
	p := &PostProcessor{}
	if err := p.Configure(); err != nil {
		t.Fatalf("Configure() returned error: %v", err)
	}
}

func TestPostProcessor_PostProcess_NoError(t *testing.T) {
	p := &PostProcessor{}
	ui := &packersdk.BasicUi{Writer: io.Discard, ErrorWriter: io.Discard}
	art := &packersdk.MockArtifact{}
	out, keep, push, err := p.PostProcess(context.Background(), ui, art)
	if err != nil {
		t.Fatalf("PostProcess() returned error: %v", err)
	}
	if out != art {
		t.Fatal("expected same artifact returned")
	}
	if !keep {
		t.Fatal("expected keep_input_artifact true")
	}
	if push {
		t.Fatal("expected push false")
	}
}
