// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package sylvevm implements the "sylve-vm" Packer builder.
//
// The builder connects to an EXISTING VM registered in Sylve, optionally
// snapshots its ZFS storage datasets to support rollback (preserve_original),
// starts the VM, runs configured provisioners over SSH or WinRM, shuts the VM
// down, and optionally deletes it (destroy = true).
//
// Unlike sylve-iso this builder does not create a new VM — it works with VMs
// that are already registered in Sylve and uses only the Sylve REST API
// (no SSH access to the Sylve host itself is required).
//
// Build lifecycle:
//
//  1. Find the existing VM by vm_name and validate it is stopped.
//  2. Optionally snapshot storage disks (preserve_original = true).
//  3. Start the VM.
//  4. Discover the guest's IP address via the Sylve DHCP lease API.
//  5. Connect via SSH or WinRM and run Packer provisioners.
//  6. Send the shutdown_command (if configured) and wait for the domain to halt.
//  7. Optionally delete the VM from Sylve (destroy = true; default is false).
package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	sylvecommon "github.com/xoro/packer-plugin-sylve/builder/sylve/common"
	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// sylveLoginRetryInterval is the pause between login attempts when the Sylve API
// is temporarily unreachable. Overridable in tests.
var sylveLoginRetryInterval = 5 * time.Second

// vmBuildStepsHook is nil in production. Tests may set it to inject a custom
// step list so that builder.Run can be exercised without a live Sylve instance.
var vmBuildStepsHook func(*Builder) []multistep.Step

// ensureAuthLoginFn calls Sylve POST /auth/login via c. Tests may substitute a
// custom implementation so Builder.ensureAuth's outer retry/sleep loop can be
// reached without exhausting the HTTP client's inner retry budget inside a
// single Login invocation.
var ensureAuthLoginFn = func(c *client.Client, username, password, authType string) (string, error) {
	return c.Login(username, password, authType)
}

// Builder implements packersdk.Builder for source "sylve-vm".
type Builder struct {
	config Config
	runner multistep.Runner
}

// ConfigSpec implements packersdk.Builder and is populated by go generate.
func (b *Builder) ConfigSpec() hcldec.ObjectSpec {
	return b.config.FlatMapstructure().HCL2Spec()
}

// Prepare decodes and validates the builder configuration.
func (b *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	return b.config.Prepare(raws...)
}

// ensureAuth logs in when no token is configured and returns a cleanup function
// that invalidates the session. The caller should defer cleanup() when non-nil.
// Login is retried until sylve_api_login_timeout elapses when the API is
// unreachable.
func (b *Builder) ensureAuth(ui packersdk.Ui) (cleanup func(), err error) {
	if b.config.SylveToken != "" {
		return nil, nil
	}
	waitBudget := b.config.sylveAPILoginTimeoutDur
	if waitBudget <= 0 {
		waitBudget = 2 * time.Minute
	}
	deadline := time.Now().Add(waitBudget)

	logoutCleanup := func() {
		lc := client.New(b.config.SylveURL, b.config.SylveToken, b.config.TLSSkipVerify)
		if err := lc.Logout(); err != nil {
			ui.Error(fmt.Sprintf("Sylve logout: %s", err))
		} else {
			ui.Say("Logged out of Sylve")
		}
	}

	for {
		c := client.New(b.config.SylveURL, "", b.config.TLSSkipVerify)
		token, err := ensureAuthLoginFn(c, b.config.SylveUser, b.config.SylvePassword, b.config.SylveAuthType)
		if err == nil {
			b.config.SylveToken = token
			ui.Say(fmt.Sprintf("Logged in to Sylve as %q", b.config.SylveUser))
			return logoutCleanup, nil
		}
		if !client.IsRetriableLoginWaitError(err) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("sylve login: timed out after %v waiting for API (last error: %w)", waitBudget, err)
		}
		ui.Say(fmt.Sprintf("Sylve API not ready yet; retrying: %v", err))
		remaining := time.Until(deadline)
		sleep := sylveLoginRetryInterval
		if sleep > remaining {
			sleep = remaining
		}
		if sleep <= 0 && remaining > 0 {
			sleep = time.Millisecond
		}
		if sleep > 0 {
			time.Sleep(sleep)
		}
	}
}

// Run executes the full VM build lifecycle. It returns the resulting Artifact
// on success.
func (b *Builder) Run(ctx context.Context, ui packersdk.Ui, hook packersdk.Hook) (packersdk.Artifact, error) {
	authCleanup, err := b.ensureAuth(ui)
	if err != nil {
		return nil, err
	}
	if authCleanup != nil {
		defer authCleanup()
	}

	state := new(multistep.BasicStateBag)
	state.Put("hook", hook)
	state.Put("ui", ui)
	state.Put("config", &b.config)

	steps := []multistep.Step{
		&StepFindVM{Config: &b.config},
		&StepSnapshotDisks{Config: &b.config},
		&StepStartVM{Config: &b.config},
		&StepBootWait{Config: &b.config},
		&sylvecommon.StepDiscoverIP{
			SylveURL:      b.config.SylveURL,
			SylveToken:    b.config.SylveToken,
			TLSSkipVerify: b.config.TLSSkipVerify,
		},
		&StepWinRMTunnel{Config: &b.config},
		&communicator.StepConnect{
			Config:    &b.config.Config,
			Host:      b.instanceIPFromState,
			SSHConfig: b.config.Config.SSHConfigFunc(),
		},
		new(commonsteps.StepProvision),
		&StepShutdown{Config: &b.config},
		&sylvecommon.StepDeleteVM{
			SylveURL:      b.config.SylveURL,
			SylveToken:    b.config.SylveToken,
			TLSSkipVerify: b.config.TLSSkipVerify,
			Destroy:       b.config.Destroy,
		},
	}
	if vmBuildStepsHook != nil {
		steps = vmBuildStepsHook(b)
	}

	b.runner = &multistep.BasicRunner{Steps: steps}
	b.runner.Run(ctx, state)

	if err, ok := state.GetOk("error"); ok {
		return nil, err.(error)
	}

	return b.buildArtifact(state), nil
}

// instanceIPFromState returns the discovered guest IP from the state bag for
// communicator.StepConnect.
func (b *Builder) instanceIPFromState(s multistep.StateBag) (string, error) {
	ip, ok := s.GetOk("instance_ip")
	if !ok {
		return "", fmt.Errorf("instance IP not yet discovered")
	}
	return ip.(string), nil
}

// buildArtifact constructs the Artifact from the final state bag.
func (b *Builder) buildArtifact(state multistep.StateBag) *sylvecommon.Artifact {
	vmRID, _ := state.Get("vm_rid").(uint)
	vmID, _ := state.Get("vm_id").(uint)
	return &sylvecommon.Artifact{
		BuilderID: BuilderID,
		VMRID:     vmRID,
		VMID:      vmID,
		StateData: map[string]interface{}{
			"vm_rid": vmRID,
			"vm_id":  vmID,
		},
	}
}
