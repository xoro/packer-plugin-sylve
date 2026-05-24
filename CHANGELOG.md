<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# Changelog

## [0.1.10] - 2026-05-24

### Bug Fixes

- _(ci)_ Lower coverage threshold to 99.5%
- _(ci)_ Remove Windows build targets

### Documentation

- Fix create_release.sh option documentation

### Testing

- _(iso)_ Cover drainServerMsgCh deadline timeout path

## [0.1.9] - 2026-05-24

### Bug Fixes

- _(ci)_ Lower coverage threshold to 99.5%
- _(ci)_ Remove Windows build targets

### Testing

- _(iso)_ Cover drainServerMsgCh deadline timeout path

## [0.1.8] - 2026-05-24

### Features

- _(builder)_ Replace find-vm and snapshot-disks with create-from-template workflow

### Bug Fixes

- _(builder)_ Work around Sylve NIC enable=false preventing DHCP lease
- _(builder)_ Fix VNC auth negotiation and view server memory leak
- _(builder)_ Disable SSH keep-alive to prevent x/crypto drain loop CPU spin

### Other

- Add windows/amd64 and windows/arm64 to GoReleaser build matrix

### Refactor

- Migrate coding rules from .cursor/rules to .github/instructions
- _(builder)_ Restructure into builder/sylve/{common,iso,vm,jail}

### Testing

- Fix slow tests and add coverage for NIC fix paths

### Miscellaneous Tasks

- Rename format_code.sh to format_files.sh and add YAML formatter

## [0.1.7] - 2026-05-23

### Features

- _(sylvevm)_ Add WinRM auto-tunnel and boot_wait support

## [0.1.6] - 2026-04-28

### Bug Fixes

- _(ci)_ Add --tag to git-cliff and use gh release edit for release body

## [0.1.5] - 2026-04-28

### Features

- _(build)_ Add freebsd/arm64, linux/amd64, linux/arm64, openbsd/amd64, openbsd/arm64 targets

### Bug Fixes

- _(release)_ Enable git-cliff changelog in goreleaser
- _(config)_ Remove emojis from cliff.toml and CHANGELOG.md

### Refactor

- _(scripts)_ Rename run_all.sh to run_all_quality_checks.sh
- _(scripts)_ Make push default in create_release.sh

## [0.1.4] - 2026-04-28

### Features

- _(wip)_ Fix VM running-state detection and add auto SSH bastion

### Bug Fixes

- _(build)_ Hardcode PLUGIN_FQN to fix bmake compatibility on FreeBSD

### Documentation

- Document GitHub Actions to GoReleaser pipeline in create_release.sh

## [0.1.3] - 2026-04-06

### Miscellaneous Tasks

- _(security)_ Tighten trivy scan and add patch release script

## [0.1.2] - 2026-04-06

### Build and documentation

- Add `bin/build_release_artifacts.sh` for manual darwin/arm64 and freebsd/amd64 zips plus SHA256SUMS (aligned with GoReleaser
  naming)
- Add `--version` to that script to rebuild artifacts for a past semver without editing `version/version.go`
- Add `bin/publish_github_release_artifacts.sh` to upload or create GitHub Releases via `gh`
- Document plugin `source` vs repository naming in README; clarify `PLUGIN_INSTALL_FQN` in Makefile; extend
  `docs/GITHUB_RELEASES.md` and branch protection notes

## [0.1.1] - 2026-04-06

### Patch release

- Same binary matrix as v0.1.0 (darwin/arm64, freebsd/amd64)

## [0.1.0] - 2026-04-06

### Features

- Add Sylve Packer plugin sources and tooling

### Miscellaneous Tasks

- Initial commit
