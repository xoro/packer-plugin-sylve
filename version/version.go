// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Package version defines version constants for the packer-plugin-sylve binary.
// These values are embedded in the Packer init lock file and reported by
// "packer plugins installed".
package version

import "github.com/hashicorp/packer-plugin-sdk/version"

var (
	// Version is the semantic version of the plugin (MAJOR.MINOR.PATCH), without a "v"
	// prefix. Git release tags use vX.Y.Z per HashiCorp Packer convention.
	Version = "0.1.0"

	// VersionPrerelease is an optional pre-release label (e.g. "dev", "beta.1").
	// An empty string means this is a production release.
	VersionPrerelease = ""

	// VersionMetadata is optional build metadata appended to the version string.
	VersionMetadata = ""
)

// PluginVersion is the Packer SDK version object constructed from the constants
// above. It is passed to plugin.Set.SetVersion during plugin registration.
var PluginVersion = version.NewPluginVersion(Version, VersionPrerelease, VersionMetadata)
