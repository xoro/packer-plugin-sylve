<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# Changelog

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
