// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package version

import "testing"

func TestPluginVersion_String(t *testing.T) {
	if PluginVersion == nil {
		t.Fatal("PluginVersion must be set")
	}
	s := PluginVersion.String()
	if s == "" {
		t.Fatal("expected non-empty version string")
	}
}

func TestVersionConstants(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must be non-empty")
	}
}
