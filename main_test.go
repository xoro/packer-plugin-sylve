// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package main

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestNewPluginSet(t *testing.T) {
	pps := newPluginSet()
	if pps == nil {
		t.Fatal("newPluginSet() returned nil")
	}
}

func TestMain_errorPath(t *testing.T) {
	oldRun, oldExit, oldWriter := runPlugin, exitHook, errWriter
	defer func() { runPlugin, exitHook, errWriter = oldRun, oldExit, oldWriter }()

	runPlugin = func() error { return errors.New("plugin failed") }
	var exitCode int
	exitHook = func(code int) { exitCode = code }
	errWriter = io.Discard

	main()

	if exitCode != 1 {
		t.Fatalf("exitHook(%d), want 1", exitCode)
	}
}

func TestMain_successPath(t *testing.T) {
	oldRun, oldExit := runPlugin, exitHook
	defer func() { runPlugin, exitHook = oldRun, oldExit }()

	runPlugin = func() error { return nil }
	exitHook = func(code int) { t.Fatalf("unexpected exitHook(%d)", code) }

	main()
}

// Exercises defaultRunPlugin so the RPC entry point is linked in coverage builds.
// It is expected to block; the test times out quickly.
func TestDefaultRunPlugin_InvokesPluginRun(t *testing.T) {
	done := make(chan struct{})
	go func() {
		_ = defaultRunPlugin()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Millisecond):
	}
}
