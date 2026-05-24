// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylveiso

import (
	"context"
	"testing"

	vnc "github.com/mitchellh/go-vnc"
)

// TestRunFramebufferPollerInner_StoppedReturnsEarly verifies that
// runFramebufferPollerInner returns immediately when the server is stopped,
// covering the early-return branch at the top of the function.
func TestRunFramebufferPollerInner_StoppedReturnsEarly(t *testing.T) {
	ss := &vncViewServer{stopped: true}
	ch := make(chan vnc.ServerMessage)
	// Should return immediately without blocking.
	ss.runFramebufferPollerInner(context.Background(), ch, false)
}
