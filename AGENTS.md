# packer-plugin-sylve

A [HashiCorp Packer](https://developer.hashicorp.com/packer) multi-component plugin for
[Sylve](https://github.com/AlchemillaHQ/Sylve) — a lightweight, open-source FreeBSD
management platform for Bhyve VMs, FreeBSD Jails, and ZFS storage.

The plugin targets the Sylve REST API (base path `https://<host>:8181`) to create and
manage Bhyve VM images on FreeBSD 15.0+.

## Architecture

Follows [packer-plugin-scaffolding](https://github.com/hashicorp/packer-plugin-scaffolding) conventions:

```text
builder/sylve/         # Builder: creates Bhyve VM images via the Sylve API
provisioner/sylve/     # Provisioner: configures a running guest
post-processor/sylve/  # Post-processor: processes artifacts after build
datasource/sylve/      # Data source: queries existing Sylve resources
version/               # Version constants (used by goreleaser ldflags)
main.go                # Plugin entry point — registers all components
Makefile               # Build, install, and test targets
docs/                  # MDX component docs for the Packer integration portal
example/               # HCL2 (.pkr.hcl) usage examples
```

## Build and Test

```sh
# Build
go build ./...

# Build dev binary and install into Packer's plugin path
make dev

# Unit tests
go test ./...

# Acceptance tests (requires a live Sylve instance)
PACKER_ACC=1 go test -count 1 -v ./... -timeout=120m
```

## Releases (GoReleaser / CI)

- **Config**: [`.goreleaser.yml`](.goreleaser.yml) — cross-builds **darwin/arm64** and **freebsd/amd64**, ldflags for
  `version.Version` / `version.VersionPrerelease`, `API_VERSION=x5.0` (must match
  [`packer-plugin-sdk`](https://github.com/hashicorp/packer-plugin-sdk) plugin API; bump when the SDK changes).
- **Git tags**: `vX.Y.Z` (HashiCorp style).
- **GitHub Actions**: [`.github/workflows/release.yml`](.github/workflows/release.yml) runs GoReleaser on `v*` tags;
  [`.github/workflows/ci.yml`](.github/workflows/ci.yml) runs `go test` on pushes and PRs.
- **Local**: `make goreleaser-check` validates the config; `make goreleaser-snapshot` builds into `dist/` (snapshot
  mode; install the `goreleaser` CLI first).
- **Manual GitHub uploads (v0.1.x)**: see [`docs/GITHUB_RELEASES.md`](docs/GITHUB_RELEASES.md); **branch protection**:
  [`docs/BRANCH_PROTECTION.md`](docs/BRANCH_PROTECTION.md).
- **Patch release from `main`**: [`bin/create_release.sh`](bin/create_release.sh) (bumps PATCH, updates `CHANGELOG.md`,
  commits, tags `vX.Y.Z`, pushes; see [development-workflow](.cursor/rules/development-workflow.mdc)).

Acceptance tests require `SYLVE_URL` and `SYLVE_TOKEN` to be set (see Environment Variables).

## Conventions

### SDK

All components use `github.com/hashicorp/packer-plugin-sdk` (>= v0.5.2):

- Decode config structs with `github.com/mitchellh/mapstructure` + `hcldec` — always implement `ConfigSpec()`.
- Implement builder `Run()` using the `multistep` package; each discrete action is its own `Step`.
- Reuse `multistep/commonsteps` helpers (SSH communicator, boot commands, ISO steps) before writing custom steps.
- `Prepare()` must have no side effects: validate and decode config only, no API calls or resource creation.
- Honor `ctx.Done()` in every long-running operation; never block on cancellation.

### Builder ID

The builder ID (`BuilderId` on the Artifact) must follow the format `<namespace>.sylve` and
**must never change after the first public release**, as post-processors use it to identify
compatible artifacts.

### Sylve API

The Sylve REST API contract is defined in `docs/swagger/swagger.yaml` in the
[Sylve repository](https://github.com/AlchemillaHQ/Sylve). Keep an internal API client
under `internal/client/` and avoid scattering raw HTTP calls across components.

### HCL2 first

Write all template examples in HCL2 (`.pkr.hcl`). Legacy JSON templates are secondary.

## Coding Standards and Rules

All coding standards, best practices, and development guidelines are defined in the `.cursor/rules/`
directory. **Every coding agent must read and follow the relevant rule files before making any
change.**

### Rule Files Reference

| Rule File                                     | Purpose                                                         |
| --------------------------------------------- | --------------------------------------------------------------- |
| `.cursor/rules/coding-agent-guidelines.mdc`   | **Mandatory**: guidelines for agents creating or modifying code |
| `.cursor/rules/general-coding-standards.mdc`  | Universal standards for all file types (no emojis, plain ASCII) |
| `.cursor/rules/git-commit.mdc`                | Conventional commit format and approval workflow                |
| `.cursor/rules/license-header.mdc`            | BSD-2-Clause header requirements per file type                  |
| `.cursor/rules/shell-scripts.mdc`             | POSIX shell, step pattern, log format                           |
| `.cursor/rules/development-workflow.mdc`      | Branch strategy, release process                                |
| `.cursor/rules/test-scripts.mdc`              | Script inventory, retry workflow                                |
| `.cursor/rules/command-line-options.mdc`      | Long-form CLI options                                           |
| `.cursor/rules/command-output.mdc`            | Show complete unfiltered output                                 |
| `.cursor/rules/terminal.mdc`                  | Terminal usage                                                  |
| `.cursor/rules/vulnerability-remediation.mdc` | Pen test remediation workflow                                   |
| `.cursor/rules/cursor-rule-standards.mdc`     | Standards for the rule files themselves                         |

### Key Principles

1. **No Emojis**: Never use emojis in code, config, or data files (only in `.md`/`.mdc` docs)
2. **Conventional Commits**: All commits must follow the conventional commit format
3. **Long-Form Options**: Use `--verbose` not `-v` in shell scripts and documentation
4. **BSD-2-Clause License**: All source files must have license headers
5. **Development Branch**: All work happens on `development`, never commit directly to `main`
6. **POSIX Shell**: All shell scripts use `#!/bin/sh`, never `#!/bin/bash`
7. **Format Before Commit**: Run `./bin/format_code.sh` before every commit

### Quality Checks (Run Before Every Commit)

```bash
./bin/format_code.sh          # Apply formatting to all file types
./bin/run_format_checks.sh    # Verify formatting is correct
./bin/run_linter_checks.sh    # Run all linters
go test ./...                 # Run unit tests
```

For a full local gate (format check, lint, unit tests with coverage, gitleaks, security scanners), run:

```bash
./bin/run_all_quality_checks.sh
```

For **manual** GitHub release bundles (same layout as GoReleaser for v0.1.x tags where Actions skips GoReleaser), see
[`docs/GITHUB_RELEASES.md`](docs/GITHUB_RELEASES.md). Run `./bin/build_release_artifacts.sh` to produce the zips and checksum
file; run `./bin/publish_github_release_artifacts.sh --version <X.Y.Z>` to build (optional) and upload with **`gh`**.

## Environment Variables

| Variable      | Description                                               |
| ------------- | --------------------------------------------------------- |
| `SYLVE_URL`   | Sylve instance base URL, e.g. `https://freebsd-host:8181` |
| `SYLVE_TOKEN` | API authentication token                                  |
| `PACKER_ACC`  | Set to `1` to enable acceptance tests                     |
