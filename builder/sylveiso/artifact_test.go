// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"testing"
)

func TestArtifact_BuilderId(t *testing.T) {
	a := &Artifact{}
	if got := a.BuilderId(); got != BuilderID {
		t.Errorf("BuilderId() = %q, want %q", got, BuilderID)
	}
}

func TestArtifact_BuilderID_Value(t *testing.T) {
	if BuilderID != "xoro.sylveiso" {
		t.Errorf("BuilderID = %q, want %q", BuilderID, "xoro.sylveiso")
	}
}

func TestArtifact_Files(t *testing.T) {
	a := &Artifact{}
	if files := a.Files(); files != nil {
		t.Errorf("Files() = %v, want nil (VM lives on the Sylve host)", files)
	}
}

func TestArtifact_Id(t *testing.T) {
	a := &Artifact{VMRID: 42}
	if got := a.Id(); got != "42" {
		t.Errorf("Id() = %q, want %q", got, "42")
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
	a := &Artifact{
		StateData: map[string]interface{}{
			"vm_id": 99,
		},
	}
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
		t.Errorf("Destroy() = %v, want nil", err)
	}
}
