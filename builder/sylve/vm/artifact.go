// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

package vm

// BuilderID is the unique identifier for artifacts produced by the sylve-vm
// builder. Post-processors use this value to filter for compatible artifacts.
//
// BuilderID must never change after the first public release. Downstream tools
// use it to identify compatible artifacts.
const BuilderID = "xoro.sylvevm"
