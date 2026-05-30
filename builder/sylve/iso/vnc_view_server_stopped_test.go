// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package iso

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

// TestStartPoller_StoppedDoesNotLaunch verifies startPoller is a no-op once the
// server is stopped, so Wait returns immediately with no tracked goroutine.
func TestStartPoller_StoppedDoesNotLaunch(t *testing.T) {
	ss := &vncViewServer{stopped: true}
	ch := make(chan vnc.ServerMessage)
	ss.startPoller(context.Background(), ch)
	ss.Wait()
}

// TestSpawnReconnect_StoppedDoesNotLaunch verifies spawnReconnect is a no-op
// once the server is stopped, and also when no reconnect function is set.
func TestSpawnReconnect_StoppedDoesNotLaunch(t *testing.T) {
	ssStopped := &vncViewServer{stopped: true}
	ssStopped.spawnReconnect(context.Background())
	ssStopped.Wait()

	// Not stopped but no reconnect func / ui configured: also a no-op.
	ssNoFunc := &vncViewServer{}
	ssNoFunc.spawnReconnect(context.Background())
	ssNoFunc.Wait()
}
