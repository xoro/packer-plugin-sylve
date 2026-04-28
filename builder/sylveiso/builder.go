// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package sylveiso implements the "sylve-iso" Packer builder.
//
// The builder creates a Bhyve virtual machine via the Sylve REST API, boots it
// from an ISO image fetched by Sylve's built-in download manager, runs
// configured provisioners over SSH, and produces a Sylve VM artifact.
//
// Build lifecycle:
//
//  1. Download the ISO (or reuse a cached copy) via the Sylve download manager.
//  2. Start an HTTP server to serve http_directory / http_content to the guest.
//  3. Create the VM via POST /api/vm.
//  4. Start the VM and send VNC boot commands through Sylve's WebSocket proxy.
//  5. Wait for the installer to reboot the guest, disable the ISO storage, and
//     restart the VM so it boots into the freshly installed OS.
//  6. Discover the VM's IP address via the Sylve DHCP lease API.
//  7. Connect via SSH and run Packer provisioners.
//  8. Send the shutdown_command and wait for the domain to halt.
//  9. Optionally delete the VM from Sylve after success (destroy = true; default
//     is false — keep the VM).
package sylveiso

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/xoro/packer-plugin-sylve/internal/client"
)

// isoBuildStepsHook is set by tests in this package to replace the default ISO
// step list. It is only consulted when testing.Testing() is true so release
// binaries never execute injected steps.
var isoBuildStepsHook func(*Builder) []multistep.Step

// sylveLoginRetryInterval is the pause between login attempts when the Sylve API
// is temporarily unreachable. Overridable in tests.
var sylveLoginRetryInterval = 5 * time.Second

// Builder implements packersdk.Builder for source "sylve-iso".
type Builder struct {
	config Config
	runner multistep.Runner
}

// ConfigSpec implements packersdk.Builder and is populated by go generate.
func (b *Builder) ConfigSpec() hcldec.ObjectSpec {
	return b.config.FlatMapstructure().HCL2Spec()
}

// Prepare decodes and validates the builder configuration.
// It returns generalised warnings, plugin-level warnings, and an error.
func (b *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	return b.config.Prepare(raws...)
}

// ensureAuth logs in when no token is configured and returns a cleanup that
// invalidates the session. The caller should defer cleanup() when non-nil.
// When the Sylve API is not yet listening (connection refused, etc.), login is
// retried until sylve_api_login_timeout elapses.
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
		token, err := c.Login(b.config.SylveUser, b.config.SylvePassword, b.config.SylveAuthType)
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

// Run executes the full ISO build lifecycle: authentication, pre-flight checks,
// ISO download, VM creation, VNC-driven OS installation, SSH provisioning, and
// VM cleanup. It returns the resulting Artifact on success.
func (b *Builder) Run(ctx context.Context, ui packersdk.Ui, hook packersdk.Hook) (packersdk.Artifact, error) {
	authCleanup, err := b.ensureAuth(ui)
	if err != nil {
		return nil, err
	}
	if authCleanup != nil {
		defer authCleanup()
	}

	// Pre-flight: verify Sylve is initialised and the configured ZFS pool is usable.
	if err := b.checkSylveReady(ui); err != nil {
		return nil, err
	}

	// Pre-flight: verify a switch is available; create one when absent.
	if err := b.ensureSwitch(ui); err != nil {
		return nil, err
	}

	state := new(multistep.BasicStateBag)
	state.Put("hook", hook)
	state.Put("ui", ui)
	state.Put("config", &b.config)

	var steps []multistep.Step
	if isoBuildStepsHook != nil && testing.Testing() {
		steps = isoBuildStepsHook(b)
	} else {
		steps = b.defaultISOSteps()
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

func (b *Builder) defaultISOSteps() []multistep.Step {
	steps := []multistep.Step{
		// Phase A — ISO install (VNC-driven unattended install)
		&StepDownloadISO{Config: &b.config},

		// Serve http_directory so boot commands can reach {{.HTTPIP}}:{{.HTTPPort}}.
		commonsteps.HTTPServerFromHTTPConfig(&b.config.HTTPConfig),

		&StepCreateVM{Config: &b.config},
		&StepStartVM{Config: &b.config},
		&StepVNCBootCommand{Config: &b.config},
	}

	// Phase B — OS restart (only when the installer reboots before SSH is ready).
	if b.config.RestartAfterInstall {
		steps = append(steps, &StepRestartAfterInstall{Config: &b.config})
	}

	steps = append(steps,
		// Phase C — Provisioning (SSH-driven, after reboot into installed OS)
		&StepDiscoverIP{Config: &b.config},
		&communicator.StepConnect{
			Config:    &b.config.Config,
			Host:      b.instanceIPFromState,
			SSHConfig: b.config.Config.SSHConfigFunc(),
		},
		new(commonsteps.StepProvision),
		&StepShutdown{Config: &b.config},
		&StepDeleteVM{Config: &b.config},
	)

	return steps
}

func (b *Builder) buildArtifact(state multistep.StateBag) packersdk.Artifact {
	vmID, _ := state.Get("vm_id").(uint)
	// vm_rid_final is set at creation and never zeroed, so it survives
	// StepDeleteVM zeroing vm_rid on the success path.
	vmRID, _ := state.Get("vm_rid_final").(uint)
	if vmRID == 0 {
		vmRID, _ = state.Get("vm_rid").(uint)
	}
	return &Artifact{
		VMRID: vmRID,
		VMID:  vmID,
		StateData: map[string]interface{}{
			"generated_data": state.Get("generated_data"),
			"SYLVE_VM_RID":   fmt.Sprintf("%d", vmRID),
			"SYLVE_VNC_PORT": fmt.Sprintf("%d", b.config.VNCPort),
			"SYLVE_URL":      b.config.SylveURL,
		},
	}
}

// checkSylveReady verifies that Sylve has been initialised. If storage_pool
// is not configured, it is auto-set to the first pool in the basic settings.
func (b *Builder) checkSylveReady(ui packersdk.Ui) error {
	c := client.New(b.config.SylveURL, b.config.SylveToken, b.config.TLSSkipVerify)
	settings, err := c.GetBasicSettings()
	if err != nil {
		return fmt.Errorf("could not read Sylve basic settings: %w", err)
	}

	if !settings.Initialized || len(settings.Pools) == 0 {
		return fmt.Errorf(
			"sylve at %s has not been initialised yet; "+
				"please open the Sylve web UI, complete the first-time setup wizard, "+
				"and select at least one ZFS pool before running this build",
			b.config.SylveURL,
		)
	}

	if b.config.StoragePool == "" {
		b.config.StoragePool = settings.Pools[0]
		ui.Say(fmt.Sprintf("Sylve pool auto-detected: using %q", b.config.StoragePool))
		return nil
	}

	poolFound := false
	for _, p := range settings.Pools {
		if p == b.config.StoragePool {
			poolFound = true
			break
		}
	}
	if !poolFound {
		return fmt.Errorf(
			"ZFS pool %q is not registered in Sylve (available: %v); "+
				"open the Sylve web UI -> Storage -> Pools and add the pool, "+
				"or set storage_pool to one of the registered pools",
			b.config.StoragePool, settings.Pools,
		)
	}

	ui.Say(fmt.Sprintf("Sylve pool check passed: pool %q is available", b.config.StoragePool))
	return nil
}

// ensureSwitch checks whether any standard switch exists in Sylve.
// If none is found, it creates one named "Packer" with DHCP enabled and
// updates b.config.SwitchName so subsequent steps use it.
func (b *Builder) ensureSwitch(ui packersdk.Ui) error {
	c := client.New(b.config.SylveURL, b.config.SylveToken, b.config.TLSSkipVerify)
	switches, err := c.ListSwitches()
	if err != nil {
		return fmt.Errorf("could not list Sylve switches: %w", err)
	}

	// If a switch name was explicitly configured, verify it exists.
	if b.config.SwitchName != "" {
		for _, s := range switches.Standard {
			if s.Name == b.config.SwitchName {
				ui.Say(fmt.Sprintf("Sylve switch check passed: switch %q found", b.config.SwitchName))
				return nil
			}
		}
		return fmt.Errorf(
			"switch %q not found in Sylve; "+
				"available standard switches: %v; "+
				"create the switch in the Sylve web UI or update switch_name",
			b.config.SwitchName, switchNames(switches.Standard),
		)
	}

	// No switch_name configured — use or create "Packer".
	const autoSwitchName = "Packer"
	for _, s := range switches.Standard {
		if s.Name == autoSwitchName {
			ui.Say(fmt.Sprintf("Sylve switch check passed: using existing switch %q", autoSwitchName))
			b.config.SwitchName = autoSwitchName
			return nil
		}
	}

	ui.Say(fmt.Sprintf("No switches found; creating standard switch %q with DHCP enabled", autoSwitchName))
	if err := c.CreateStandardSwitch(autoSwitchName); err != nil {
		return fmt.Errorf("could not create switch %q: %w", autoSwitchName, err)
	}
	ui.Say(fmt.Sprintf("Created standard switch %q", autoSwitchName))
	b.config.SwitchName = autoSwitchName
	return nil
}

func switchNames(switches []client.StandardSwitch) []string {
	names := make([]string, len(switches))
	for i, s := range switches {
		names[i] = s.Name
	}
	return names
}
