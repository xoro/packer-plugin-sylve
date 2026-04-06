<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# Packer Plugin for Sylve

[![Latest Release](https://img.shields.io/github/v/release/xoro/packer-plugin-sylve?label=latest%20release&style=for-the-badge)][releases]
[![License](https://img.shields.io/badge/license-BSD--2--Clause-blue?style=for-the-badge)][license]

The Packer Plugin for Sylve creates machine images for [Sylve][sylve] — a lightweight,
open-source FreeBSD management platform for Bhyve VMs, FreeBSD Jails, and ZFS storage.

The plugin communicates with the Sylve REST API (default base URL `https://<host>:8181`)
to provision, configure, and snapshot virtual machines directly on a FreeBSD host.

## Components

### Builders

- [`sylve-iso`][docs-sylve-iso] — Creates a Bhyve VM, boots it from an ISO image that
  is downloaded via the Sylve download manager, runs provisioners over SSH, and
  produces an artifact from the resulting VM state. Use this builder to create a new
  image from scratch.

- `sylve-vm` _(planned)_ — Starts from an existing Sylve VM snapshot instead of
  booting from ISO, runs provisioners, and produces an updated artifact.

- `sylve-jail` _(planned)_ — Creates a FreeBSD Jail image via the Sylve API.

### Provisioner

- `sylve` _(planned)_ — Configures a running Sylve guest.

### Post-Processor

- `sylve` _(planned)_ — Processes build artifacts after the Packer build completes.

### Datasource

- `sylve` _(planned)_ — Queries existing Sylve resources for use in Packer templates.

## Requirements

**Sylve**:

A running [Sylve][sylve] instance on FreeBSD 15.0 or later is required. The plugin
connects to the Sylve REST API over HTTPS (default port `8181`). Sylve ships with a
self-signed TLS certificate; set `tls_skip_verify = true` unless you have a
trusted certificate installed.

**Go**:

- [Go 1.26.1][golang-install] or later is required to build the plugin from source.

## Installation

### Using the Releases

#### Automatic Installation

Include the following in your Packer template to automatically install the plugin when
you run `packer init`.

```hcl
packer {
  required_version = ">= 1.15.0"
  required_plugins {
    sylve = {
      version = ">= 0.1.0"
      source  = "github.com/xoro/sylve"
    }
  }
}
```

For more information, refer to the Packer [documentation][docs-packer-init].

#### Manual Installation

Install the plugin using the `packer plugins install` command:

```shell
packer plugins install github.com/xoro/sylve
```

### Using the Source

Clone the repository and run `make build`:

```shell
git clone https://github.com/xoro/packer-plugin-sylve.git
cd packer-plugin-sylve
make build
```

The `packer-plugin-sylve` binary is created in the repository root. To install it into
Packer's plugin directory, refer to the Packer
[plugin installation documentation][docs-packer-plugin-install].

For development on a FreeBSD host, `make dev` builds and installs the plugin directly
into the Packer plugin path.

## Usage

### Authentication

The `sylve-iso` builder supports two authentication methods:

- **Token-based**: set `sylve_token` or the `SYLVE_TOKEN` environment variable with a
  pre-issued Bearer token.
- **Login-based**: set `sylve_user` / `SYLVE_USER` and `sylve_password` /
  `SYLVE_PASSWORD`. The builder logs in at the start of the build and logs out when it
  finishes. Set `sylve_auth_type` to `"pam"` for PAM authentication (default is
  `"sylve"` for native database accounts).

### Example

```hcl
packer {
  required_plugins {
    sylve = {
      version = ">= 0.1.0"
      source  = "github.com/xoro/sylve"
    }
  }
}

source "sylve-iso" "freebsd" {
  sylve_url    = "https://192.168.1.10:8181"
  sylve_token  = env("SYLVE_TOKEN")

  iso_download_url = "https://download.freebsd.org/releases/amd64/amd64/ISO-IMAGES/15.0/FreeBSD-15.0-RELEASE-amd64-dvd1.iso"

  vm_name          = "freebsd-packer"
  cpu_cores        = 2
  ram              = 2048
  storage_size_mb  = 20480
  switch_name      = "packer-switch"

  boot_wait = "15s"
  boot_command = [
    "<enter>",
  ]

  ssh_username = "root"
  ssh_password = "sylve-example"
  ssh_timeout  = "30m"

  shutdown_command      = "/sbin/poweroff"
  restart_after_install = false
  destroy               = true
}

build {
  sources = ["source.sylve-iso.freebsd"]

  provisioner "shell" {
    inline = ["echo 'Build complete'"]
  }
}
```

### Key Configuration Options (`sylve-iso`)

| Option                          | Required | Default                  | Description                                                                                                                                                                                                                                                                                                                            |
| ------------------------------- | -------- | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sylve_url`                     |          | `https://localhost:8181` | Base URL of the Sylve instance                                                                                                                                                                                                                                                                                                         |
| `sylve_token`                   | one of   |                          | Pre-issued Bearer token                                                                                                                                                                                                                                                                                                                |
| `sylve_user` + `sylve_password` | one of   |                          | Login credentials                                                                                                                                                                                                                                                                                                                      |
| `iso_download_url`              | yes      |                          | URL passed to Sylve's download manager                                                                                                                                                                                                                                                                                                 |
| `switch_name`                   | yes      |                          | Name of a DHCP-enabled Sylve virtual switch                                                                                                                                                                                                                                                                                            |
| `cpu_cores`                     |          | `2`                      | Number of vCPU cores                                                                                                                                                                                                                                                                                                                   |
| `ram`                           |          | `1024`                   | Memory in MiB                                                                                                                                                                                                                                                                                                                          |
| `storage_size_mb`               |          | `65536`                  | Install disk size in MiB                                                                                                                                                                                                                                                                                                               |
| `loader`                        |          | `uefi`                   | Firmware: `uefi` or `bios`                                                                                                                                                                                                                                                                                                             |
| `boot_wait`                     |          | `10s`                    | Wait before sending VNC boot commands                                                                                                                                                                                                                                                                                                  |
| `http_directory`                |          |                          | Directory of files to serve over HTTP to the guest (accessible as `{{ .HTTPIP }}:{{ .HTTPPort }}` in `boot_command`)                                                                                                                                                                                                                   |
| `http_content`                  |          |                          | Map of URL path → content string served over HTTP; alternative to `http_directory`                                                                                                                                                                                                                                                     |
| `http_port_min`                 |          | `8000`                   | Lower bound of the port range for the built-in HTTP server                                                                                                                                                                                                                                                                             |
| `http_port_max`                 |          | `9000`                   | Upper bound of the port range for the built-in HTTP server                                                                                                                                                                                                                                                                             |
| `http_bind_address`             |          |                          | IP address the HTTP server listens on; defaults to all interfaces                                                                                                                                                                                                                                                                      |
| `restart_after_install`         |          | `false`                  | Force-stop the installer VM, disable the ISO storage, and restart before provisioning. Set to `true` for OS installers that reboot into the CD again on Bhyve auto-restart (e.g. Alpine). Leave `false` (default) for installers that update UEFI NVRAM and boot the installed disk on the next start (e.g. OpenBSD, FreeBSD, Debian). |
| `destroy`                       |          | `true`                   | Delete the VM after a successful build                                                                                                                                                                                                                                                                                                 |
| `tls_skip_verify`               |          | `true`                   | Skip TLS certificate verification                                                                                                                                                                                                                                                                                                      |

## Development

```shell
# Build
make build

# Run unit tests
go test ./...

# Run acceptance tests (requires a live Sylve instance)
PACKER_ACC=1 SYLVE_URL=https://host:8181 SYLVE_TOKEN=<token> go test -count 1 -v ./... -timeout=120m

# Format code
./bin/format_code.sh

# Run linters
./bin/run_linter_checks.sh

# Run security scanners
./bin/run_security_scanners.sh
```

## Contributing

Contributions are welcome. Please open an issue or pull request on the
[GitHub repository][repo].

## License

Copyright (c) 2026, Timo Pallach.</br>
Licensed under the [BSD 2-Clause License][license].

[license]: LICENSE
[repo]: https://github.com/xoro/packer-plugin-sylve
[releases]: https://github.com/xoro/packer-plugin-sylve/releases
[sylve]: https://github.com/AlchemillaHQ/Sylve
[golang-install]: https://golang.org/doc/install
[docs-packer-init]: https://developer.hashicorp.com/packer/docs/commands/init
[docs-packer-plugin-install]: https://developer.hashicorp.com/packer/docs/plugins/install-plugins
[docs-sylve-iso]: docs/builders/sylve-iso.mdx
