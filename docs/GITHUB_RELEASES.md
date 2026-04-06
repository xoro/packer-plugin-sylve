<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# GitHub Releases (manual artifacts)

For tags **v0.1.0** and **v0.1.1**, GoReleaser is **not** run in Actions (see `.github/workflows/release.yml`).
Build artifacts locally under `release-artifacts/<version>/` (gitignored), then attach them to a GitHub Release.

## Contents per version

Each `release-artifacts/vX.Y.Z/` directory should contain:

- `packer-plugin-sylve_vX.Y.Z_x5.0_darwin_arm64.zip`
- `packer-plugin-sylve_vX.Y.Z_x5.0_freebsd_amd64.zip`
- `packer-plugin-sylve_vX.Y.Z_SHA256SUMS`

Rebuild after changing `version/version.go` so embedded semver matches the tag.

## Publishing with GitHub CLI

From the repository root (with `gh` authenticated and permission to create releases):

```sh
gh release create v0.1.0 --repo xoro/packer-plugin-sylve --title "v0.1.0" --notes-file CHANGELOG.md \
  release-artifacts/v0.1.0/packer-plugin-sylve_v0.1.0_x5.0_darwin_arm64.zip \
  release-artifacts/v0.1.0/packer-plugin-sylve_v0.1.0_x5.0_freebsd_amd64.zip \
  release-artifacts/v0.1.0/packer-plugin-sylve_v0.1.0_SHA256SUMS
```

Repeat for **v0.1.1** with the `v0.1.1` paths and tag. If `gh release create` fails with a permissions or scope
error, create the release in the UI and upload the same files.

## From v0.1.2 onward

Pushing a tag **other than** v0.1.0 / v0.1.1 triggers **GoReleaser** in Actions; manual zips are optional.
