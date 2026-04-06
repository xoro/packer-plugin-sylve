// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylvevm

import (
	"context"
	"io"
	"testing"

	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestBuilder_ConfigSpec_NotNil(t *testing.T) {
	b := &Builder{}
	if b.ConfigSpec() == nil {
		t.Error("ConfigSpec() returned nil")
	}
}

func TestBuilder_Prepare_NoError(t *testing.T) {
	b := &Builder{}
	_, _, err := b.Prepare()
	if err != nil {
		t.Fatalf("Prepare() returned error: %v", err)
	}
}

func TestBuilderID_Value(t *testing.T) {
	if BuilderID != "xoro.sylvevm" {
		t.Errorf("BuilderID = %q, want %q", BuilderID, "xoro.sylvevm")
	}
}

func TestBuilder_Run_ReturnsNilArtifact(t *testing.T) {
	b := &Builder{}
	ui := &packersdk.BasicUi{Writer: io.Discard, ErrorWriter: io.Discard}
	art, err := b.Run(context.Background(), ui, nil)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if art != nil {
		t.Fatal("expected nil artifact from stub builder")
	}
}
