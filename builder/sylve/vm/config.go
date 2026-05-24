// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

//go:generate packer-sdc mapstructure-to-hcl2 -type Config

package vm

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

// Config is the configuration for the sylve-vm builder.
//
// The sylve-vm builder creates a new VM from a named Sylve template, starts it,
// runs provisioners via SSH or WinRM, shuts the VM down, and either keeps or
// destroys it based on keep_registered. It never modifies the Sylve host via
// SSH — all interaction uses the Sylve REST API.
type Config struct {
	common.PackerConfig `mapstructure:",squash"`
	communicator.Config `mapstructure:",squash"`

	// SylveURL is the base URL of the Sylve instance.
	// Defaults to "https://localhost:8181".
	SylveURL string `mapstructure:"sylve_url"`

	// SylveToken is a pre-issued Bearer token for the Sylve API.
	// Supply this OR (SylveUser + SylvePassword). Falls back to the SYLVE_TOKEN
	// environment variable.
	SylveToken string `mapstructure:"sylve_token"`

	// SylveUser is the Sylve account username used for login-based auth.
	// Falls back to the SYLVE_USER environment variable.
	// Required when sylve_token is not set.
	SylveUser string `mapstructure:"sylve_user"`

	// SylvePassword is the Sylve account password used for login-based auth.
	// Falls back to the SYLVE_PASSWORD environment variable.
	// Required when sylve_token is not set.
	SylvePassword string `mapstructure:"sylve_password"`

	// SylveAuthType is the Sylve authentication type sent in the login request.
	// Valid values: "sylve" (native DB account, default) or "pam" (PAM auth).
	// Falls back to the SYLVE_AUTH_TYPE environment variable.
	SylveAuthType string `mapstructure:"sylve_auth_type"`

	// SylveAPILoginTimeout is how long to keep retrying password login when the
	// Sylve API is unreachable. HCL duration, e.g. "2m", "5m". Defaults to 2m.
	// Override with SYLVE_API_LOGIN_TIMEOUT when unset in HCL.
	SylveAPILoginTimeout string `mapstructure:"sylve_api_login_timeout"`

	// sylveAPILoginTimeoutDur is set in Prepare from SylveAPILoginTimeout.
	sylveAPILoginTimeoutDur time.Duration

	// TLSSkipVerify disables TLS certificate verification.
	// Defaults to true because Sylve ships a self-signed certificate.
	// [SECURITY DESIGN] intentional: default is insecure-skip-verify so
	// out-of-box Sylve installations work without manual certificate trust.
	TLSSkipVerify bool `mapstructure:"tls_skip_verify"`

	// SourceTemplate is the name of the Sylve template to create the VM from.
	// Required.
	SourceTemplate string `mapstructure:"source_template"`

	// VMName is the name assigned to the newly created VM. Supports Packer
	// template variables: {{build_type}}, {{build_name}}, {{uuid}}.
	// Required.
	VMName string `mapstructure:"vm_name"`

	// ShutdownCommand is the command used to shut down the VM gracefully before
	// the builder finishes. The command is sent via the communicator (SSH or
	// WinRM). When empty, no shutdown command is sent — the builder calls the
	// Sylve StopVM API directly. Leave empty for Windows guests where WinRM-based
	// shutdown is handled by boot_command or scripts.
	ShutdownCommand string `mapstructure:"shutdown_command"`

	// KeepRegistered controls whether the VM remains in Sylve after the build.
	// Defaults to true. When false, the VM and its disks are deleted after a
	// successful build.
	KeepRegistered bool `mapstructure:"keep_registered"`

	// BootWait is the duration to wait after the VM's IP is discovered before
	// attempting the communicator connection. Useful for Windows guests that need
	// time to start WinRM after the DHCP lease becomes visible. HCL duration,
	// e.g. "1m", "30s". Defaults to empty (no wait).
	BootWait string `mapstructure:"boot_wait"`

	ctx interpolate.Context
}

// bootWaitDuration parses BootWait as a time.Duration.
// Returns 0 when BootWait is empty.
func (c *Config) bootWaitDuration() (time.Duration, error) {
	if c.BootWait == "" {
		return 0, nil
	}
	return time.ParseDuration(c.BootWait)
}

// Prepare validates and normalises the configuration.
// No side effects — no API calls, no file creation.
func (c *Config) Prepare(raws ...interface{}) ([]string, []string, error) {
	err := config.Decode(c, &config.DecodeOpts{
		PluginType:         "packer.builder.sylve-vm",
		Interpolate:        true,
		InterpolateContext: &c.ctx,
		InterpolateFilter:  &interpolate.RenderFilter{},
	}, raws...)
	if err != nil {
		return nil, nil, err
	}

	var errs *packersdk.MultiError

	// --- Auth defaults ---

	if c.SylveURL == "" {
		if host := os.Getenv("SYLVE_HOST"); host != "" {
			c.SylveURL = "https://" + host + ":8181"
		} else {
			c.SylveURL = "https://localhost:8181"
		}
	}
	if c.SylveToken == "" {
		c.SylveToken = os.Getenv("SYLVE_TOKEN")
	}
	if c.SylveUser == "" {
		c.SylveUser = os.Getenv("SYLVE_USER")
	}
	if c.SylvePassword == "" {
		c.SylvePassword = os.Getenv("SYLVE_PASSWORD")
	}
	if c.SylveAuthType == "" {
		c.SylveAuthType = os.Getenv("SYLVE_AUTH_TYPE")
	}
	if c.SylveAuthType == "" {
		c.SylveAuthType = "sylve"
	}
	if c.SylveAPILoginTimeout == "" {
		if env := os.Getenv("SYLVE_API_LOGIN_TIMEOUT"); env != "" {
			c.SylveAPILoginTimeout = env
		}
	}
	loginWait := 2 * time.Minute
	if c.SylveAPILoginTimeout != "" {
		d, parseErr := time.ParseDuration(c.SylveAPILoginTimeout)
		if parseErr != nil {
			errs = packersdk.MultiErrorAppend(errs, fmt.Errorf("invalid sylve_api_login_timeout: %w", parseErr))
		} else if d < 0 {
			errs = packersdk.MultiErrorAppend(errs, errors.New("sylve_api_login_timeout must be >= 0"))
		} else {
			loginWait = d
		}
	}
	c.sylveAPILoginTimeoutDur = loginWait

	// TLSSkipVerify defaults to true because Sylve ships a self-signed cert.
	// The bool zero-value is false, so we set it unconditionally here. Users
	// who present a CA-signed certificate must set tls_skip_verify = false.
	if !c.TLSSkipVerify {
		c.TLSSkipVerify = true
	}

	// --- VM defaults ---

	// KeepRegistered defaults to false (VM is deleted after a successful build).
	// A bool cannot distinguish "unset" from "false", so no override is applied.

	// --- Validation ---

	if c.SylveToken == "" && (c.SylveUser == "" || c.SylvePassword == "") {
		errs = packersdk.MultiErrorAppend(errs, errors.New(
			"authentication required: provide sylve_token (or SYLVE_TOKEN) "+
				"OR both sylve_user (SYLVE_USER) and sylve_password (SYLVE_PASSWORD)"))
	}
	if c.SourceTemplate == "" {
		errs = packersdk.MultiErrorAppend(errs, errors.New("source_template is required"))
	}
	if c.VMName == "" {
		errs = packersdk.MultiErrorAppend(errs, errors.New("vm_name is required"))
	}

	// Auto-bastion: only for SSH communicator. When no explicit ssh_bastion_host
	// is set, route SSH through the Sylve host so Packer does not need a direct
	// route to the VM subnet. WinRM communicator uses StepWinRMTunnel instead.
	if (c.Config.Type == "" || c.Config.Type == "ssh") && c.Config.SSHBastionHost == "" {
		u, parseErr := url.Parse(c.SylveURL)
		if parseErr == nil && u.Hostname() != "" && !sylveHostIsLocal(u.Hostname()) {
			applyAutoBastion(c, u.Hostname())
		} else if parseErr == nil && u.Hostname() != "" {
			log.Printf("[DEBUG] sylve ssh-proxy: skipping bastion — Sylve host %s is local", u.Hostname())
		}
	}

	if _, err := c.bootWaitDuration(); err != nil {
		errs = packersdk.MultiErrorAppend(errs, fmt.Errorf("boot_wait: %w", err))
	}

	// Workaround: disable the SDK's SSH session keep-alive. In x/crypto/ssh
	// v0.52.0, channel.SendRequest has a drain loop that busy-spins when
	// ch.msg is closed (after session.Close). The SDK starts a keep-alive
	// goroutine per session; after each provisioner command finishes and the
	// session closes, the goroutine's next SendRequest enters an infinite
	// loop pegging a CPU core. With multiple provisioner commands this
	// accumulates to 400%+ CPU. Setting the interval to -1 disables the
	// goroutine entirely. TCP-level keepalive on the SSH transport and the
	// active provisioner I/O prevent idle disconnects.
	if c.Config.SSHKeepAliveInterval == 0 {
		c.Config.SSHKeepAliveInterval = -1 * time.Second
	}

	if err := c.Config.Prepare(&c.ctx); err != nil {
		errs = packersdk.MultiErrorAppend(errs, err...)
	}

	if errs != nil && len(errs.Errors) > 0 {
		return nil, nil, errs
	}
	return nil, nil, nil
}
