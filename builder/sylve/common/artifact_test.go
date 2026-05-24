// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package common

import (
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

func newTestState(t *testing.T) *multistep.BasicStateBag {
	t.Helper()
	state := new(multistep.BasicStateBag)
	state.Put("ui", packersdk.TestUi(t))
	return state
}

func TestArtifact_BuilderId(t *testing.T) {
	a := &Artifact{BuilderID: "xoro.sylveiso", VMRID: 1, VMID: 2}
	if a.BuilderId() != "xoro.sylveiso" {
		t.Errorf("BuilderId() = %q, want %q", a.BuilderId(), "xoro.sylveiso")
	}
}

func TestArtifact_Files(t *testing.T) {
	a := &Artifact{}
	if a.Files() != nil {
		t.Error("Files() should return nil for Sylve VM artifacts")
	}
}

func TestArtifact_Id(t *testing.T) {
	a := &Artifact{VMRID: 42}
	if a.Id() != "42" {
		t.Errorf("Id() = %q, want \"42\"", a.Id())
	}
}

func TestArtifact_Id_Zero(t *testing.T) {
	a := &Artifact{VMRID: 0}
	if got := a.Id(); got != "0" {
		t.Errorf("Id() = %q, want %q (zero RID)", got, "0")
	}
}

func TestArtifact_String(t *testing.T) {
	a := &Artifact{VMRID: 3, VMID: 17}
	want := "Sylve VM RID 3 (ID 17)"
	if got := a.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestArtifact_State_Present(t *testing.T) {
	a := &Artifact{StateData: map[string]interface{}{"vm_id": 99}}
	v := a.State("vm_id")
	if v == nil {
		t.Fatal(`State("vm_id") = nil, want 99`)
	}
	if v.(int) != 99 {
		t.Errorf(`State("vm_id") = %v, want 99`, v)
	}
}

func TestArtifact_State_Missing(t *testing.T) {
	a := &Artifact{StateData: map[string]interface{}{}}
	if v := a.State("nonexistent"); v != nil {
		t.Errorf(`State("nonexistent") = %v, want nil`, v)
	}
}

func TestArtifact_State_NilMap(t *testing.T) {
	a := &Artifact{}
	if v := a.State("anything"); v != nil {
		t.Errorf(`State("anything") on nil map = %v, want nil`, v)
	}
}

func TestArtifact_Destroy(t *testing.T) {
	a := &Artifact{}
	if err := a.Destroy(); err != nil {
		t.Errorf("Destroy() returned unexpected error: %v", err)
	}
}
