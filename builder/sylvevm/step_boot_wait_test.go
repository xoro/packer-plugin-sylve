// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package sylvevm

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

func TestStepBootWait_ZeroDuration_NoOp(t *testing.T) {
	step := &StepBootWait{Config: &Config{}}
	state := newTestState(t)
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
	step.Cleanup(state)
}

func TestStepBootWait_InvalidDuration_Halt(t *testing.T) {
	step := &StepBootWait{Config: &Config{BootWait: "not-a-duration"}}
	state := newTestState(t)
	if step.Run(context.Background(), state) != multistep.ActionHalt {
		t.Fatal("expected halt for invalid boot_wait")
	}
}

func TestStepBootWait_WaitsThenContinues(t *testing.T) {
	step := &StepBootWait{Config: &Config{BootWait: "20ms"}}
	state := newTestState(t)
	start := time.Now()
	if step.Run(context.Background(), state) != multistep.ActionContinue {
		t.Fatalf("err=%v", state.Get("error"))
	}
	if time.Since(start) < 15*time.Millisecond {
		t.Fatal("boot_wait did not delay")
	}
}

func TestStepBootWait_ContextCancel_Halt(t *testing.T) {
	step := &StepBootWait{Config: &Config{BootWait: "30s"}}
	state := newTestState(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if step.Run(ctx, state) != multistep.ActionHalt {
		t.Fatal("expected halt on cancelled context")
	}
}
