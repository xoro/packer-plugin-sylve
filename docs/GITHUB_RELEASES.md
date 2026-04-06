<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# GitHub Releases (manual artifacts)

For tags **v0.1.0** and **v0.1.1**, GoReleaser is **not** run in Actions (see `.github/workflows/release.yml`).
Build artifacts locally under `release-artifacts/<version>/` (gitignored), then attach them to a GitHub Release.

From the repository root, generate that directory (matching [`.goreleaser.yml`](../.goreleaser.yml) naming) with:

```sh
./bin/build_release_artifacts.sh
```

Use `--version` to build artifacts for an older release without editing
[`version/version.go`](../version/version.go) (for example **0.1.0** while the file says **0.1.1**):

```sh
./bin/build_release_artifacts.sh --version 0.1.0
```

Requires `go`, `zip`, and `sha256sum` (or `shasum` on macOS). When not using `--version`, re-run after changing
`version/version.go` so embedded semver matches the release.

## Automated upload (GitHub CLI)

With [GitHub CLI](https://cli.github.com/) installed and authenticated (`gh auth login`), from the repository root:

```sh
./bin/publish_github_release_artifacts.sh --version 0.1.0
```

This runs `./bin/build_release_artifacts.sh --version 0.1.0`, then **`gh release upload`** to attach (or replace) the three
files on the existing GitHub Release for tag **`v0.1.0`**. Use **`--no-build`** if you already built into
`release-artifacts/0.1.0/`.

**First-time release** (no GitHub Release page yet, but the git tag **`v0.1.0`** exists on the remote):

```sh
./bin/publish_github_release_artifacts.sh --version 0.1.0 --create
```

This uses **`gh release create`** with [`CHANGELOG.md`](../CHANGELOG.md) as release notes and uploads the same artifacts.
If the release already exists, omit **`--create`** and use the default upload command above.

Push the tag before publishing: `git push origin v0.1.0`.

## Contents per version

Each `release-artifacts/vX.Y.Z/` directory should contain:

- `packer-plugin-sylve_vX.Y.Z_x5.0_darwin_arm64.zip`
- `packer-plugin-sylve_vX.Y.Z_x5.0_freebsd_amd64.zip`
- `packer-plugin-sylve_vX.Y.Z_SHA256SUMS`

Rebuild after changing `version/version.go` so embedded semver matches the tag.

## Publishing with GitHub CLI (manual commands)

Prefer [`bin/publish_github_release_artifacts.sh`](../bin/publish_github_release_artifacts.sh) (see
[Automated upload](#automated-upload-github-cli)). Equivalent manual invocations:

```sh
gh release create v0.1.0 --repo xoro/packer-plugin-sylve --title "v0.1.0" --notes-file CHANGELOG.md \
  release-artifacts/v0.1.0/packer-plugin-sylve_v0.1.0_x5.0_darwin_arm64.zip \
  release-artifacts/v0.1.0/packer-plugin-sylve_v0.1.0_x5.0_freebsd_amd64.zip \
  release-artifacts/v0.1.0/packer-plugin-sylve_v0.1.0_SHA256SUMS
```

Repeat for **v0.1.1** with the `v0.1.1` paths and tag.

If `gh` fails with a permissions or scope error, create the release in the UI and upload the same files.

## From v0.1.2 onward

Pushing a tag **other than** v0.1.0 / v0.1.1 triggers **GoReleaser** in Actions; manual zips are optional.
