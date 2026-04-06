// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package main is the entry point for the packer-plugin-sylve binary.
// It registers all Sylve plugin components with the Packer plugin framework:
//
//   - builder "iso"  (sylve-iso)  — boot a Bhyve VM from an ISO and provision it
//   - builder "vm"   (sylve-vm)   — stub, planned for a future release
//   - builder "jail" (sylve-jail) — stub, planned for a future release
//   - provisioner    (default)    — stub, planned for a future release
//   - post-processor (default)    — stub, planned for a future release
//   - datasource     (default)    — stub, planned for a future release
package main

import (
	"fmt"
	"io"
	"os"

	sylveiso "github.com/xoro/packer-plugin-sylve/builder/sylveiso"
	sylvejail "github.com/xoro/packer-plugin-sylve/builder/sylvejail"
	sylvevm "github.com/xoro/packer-plugin-sylve/builder/sylvevm"
	sylveds "github.com/xoro/packer-plugin-sylve/datasource/sylve"
	sylvepp "github.com/xoro/packer-plugin-sylve/post-processor/sylve"
	sylveprov "github.com/xoro/packer-plugin-sylve/provisioner/sylve"
	"github.com/xoro/packer-plugin-sylve/version"

	"github.com/hashicorp/packer-plugin-sdk/plugin"
)

// newPluginSet registers all Sylve plugin components and returns the configured
// plugin set. Separated from main for unit testing without running the RPC server.
func newPluginSet() *plugin.Set {
	pps := plugin.NewSet()
	pps.RegisterBuilder("iso", new(sylveiso.Builder))
	pps.RegisterBuilder("vm", new(sylvevm.Builder))
	pps.RegisterBuilder("jail", new(sylvejail.Builder))
	pps.RegisterProvisioner(plugin.DEFAULT_NAME, new(sylveprov.Provisioner))
	pps.RegisterPostProcessor(plugin.DEFAULT_NAME, new(sylvepp.PostProcessor))
	pps.RegisterDatasource(plugin.DEFAULT_NAME, new(sylveds.Datasource))
	pps.SetVersion(version.PluginVersion)
	return pps
}

// defaultRunPlugin starts the Packer plugin RPC server; tests may replace runPlugin instead.
func defaultRunPlugin() error {
	return newPluginSet().Run()
}

// runPlugin is invoked by main; tests may replace it to avoid blocking on the RPC server.
var runPlugin = defaultRunPlugin

// exitHook is os.Exit; tests may replace it to avoid terminating the process.
var exitHook = os.Exit

// errWriter is os.Stderr; tests may replace it to suppress error output.
var errWriter io.Writer = os.Stderr

// main registers all plugin components and runs the Packer plugin protocol.
func main() {
	if err := runPlugin(); err != nil {
		fmt.Fprintln(errWriter, err.Error())
		exitHook(1)
	}
}
