<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# sylve-iso Examples

Packer templates that use the `sylve-iso` builder to create VM images on a
[Sylve](https://github.com/AlchemillaHQ/Sylve)-managed FreeBSD host.

Four OS examples are provided, all sharing the variables declared in
`variables.pkr.hcl`:

- `alpine.pkr.hcl`
- `debian.pkr.hcl`
- `freebsd.pkr.hcl`
- `openbsd.pkr.hcl`

## Prerequisites

- Packer >= 1.15.0
- A running [Sylve](https://github.com/AlchemillaHQ/Sylve) instance
- A DHCP-enabled virtual switch configured in Sylve

## Authentication

Choose one method:

**Token-based (recommended):**

```bash
export SYLVE_TOKEN=<your-bearer-token>
```

**Username / password:**

```bash
export SYLVE_USER=admin
export SYLVE_PASSWORD=secret
```

## Required Variables

| Variable      | Environment variable | Description                                                      |
| ------------- | -------------------- | ---------------------------------------------------------------- |
| `sylve_url`   | `SYLVE_URL`          | Base URL of the Sylve instance, e.g. `https://192.168.1.10:8181` |
| `switch_name` | `SYLVE_SWITCH`       | Name of a DHCP-enabled Sylve virtual switch                      |

## Running with the Helper Script

The `bin/run_example.sh` script builds the plugin from source, installs it
into the local Packer plugin cache, and runs the chosen example in one step.

```bash
# Set required environment variables
export SYLVE_URL=https://192.168.1.10:8181
export SYLVE_SWITCH=packer-switch

# Run an example (alpine | debian | freebsd)
./bin/run_example.sh alpine
```

## Optional Variables

All optional variables have defaults that can be overridden via `-var` flags
passed to `bin/run_example.sh` or `packer build`.

| Variable          | Default         | Description                                                                                   |
| ----------------- | --------------- | --------------------------------------------------------------------------------------------- |
| `cpu_cores`       | `2`             | Number of vCPU cores                                                                          |
| `ram`             | `2048`          | Memory in MiB                                                                                 |
| `storage_size_mb` | `20480`         | Install disk size in MiB                                                                      |
| `ssh_password`    | `sylve-example` | Root password set on the installed system (default avoids log redaction of `packer-plugin-*`) |
| `boot_wait`       | `25s`           | Time to wait after VM starts before sending boot commands                                     |
| `ssh_timeout`     | `60m`           | Maximum time to wait for SSH after install reboots                                            |
| `keep_on_error`   | `false`         | Keep VM alive on build failure for post-failure debugging                                     |

OS-specific version variables:

| Variable          | Default        | Description                     |
| ----------------- | -------------- | ------------------------------- |
| `alpine_version`  | `3.21.3`       | Alpine Linux version to install |
| `debian_version`  | `13.4.0`       | Debian version to install       |
| `freebsd_version` | `15.0-RELEASE` | FreeBSD version to install      |
| `openbsd_version` | `7.8`          | OpenBSD version to install      |

## Debugging

Enable verbose Packer logging:

```bash
PACKER_LOG=1 packer build -var "sylve_url=..." -var "switch_name=..." .
```

Set `keep_on_error=true` to leave the VM running after a failed build so you
can inspect the state:

```bash
packer build -var "keep_on_error=true" -var "sylve_url=..." -var "switch_name=..." .
```
