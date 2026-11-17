// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package common

import "testing"

func TestVMName(t *testing.T) {
	for in, want := range map[string]string{
		"flyingmonit-clone-openbsd-8.0-amd64": "flyingmonit-clone-openbsd-8_0-amd64",
		"packer_vm-1":                         "packer_vm-1",
		"a b..c/d":                            "a_b_c_d",
	} {
		if got := VMName(in); got != want {
			t.Errorf("VMName(%q) = %q, want %q", in, got, want)
		}
	}
}
