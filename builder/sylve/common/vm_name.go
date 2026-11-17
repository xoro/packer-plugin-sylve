// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package common

import "regexp"

// invalidVMNameRuns matches the characters Sylve rejects in VM names: it
// accepts only ^[a-zA-Z0-9-_]+$ and answers invalid_vm_name otherwise.
var invalidVMNameRuns = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// VMName returns name with every run of characters Sylve rejects in VM names
// (anything but a-z, A-Z, 0-9, _ and -) replaced by "_", so that e.g.
// "base-8.0" becomes "base-8_0". It has no side effects.
func VMName(name string) string {
	return invalidVMNameRuns.ReplaceAllString(name, "_")
}
