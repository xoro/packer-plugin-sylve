// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package iso

import (
	"testing"
)

func TestBuilderID_Value(t *testing.T) {
	if BuilderID != "xoro.sylveiso" {
		t.Errorf("BuilderID = %q, want %q", BuilderID, "xoro.sylveiso")
	}
}
