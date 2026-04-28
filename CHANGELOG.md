<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# Changelog

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
