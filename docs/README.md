<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

<!-- markdownlint-disable first-line-h1 no-inline-html -->

The Packer Plugin for Sylve creates machine images using
[Sylve][sylve] — a lightweight, open-source FreeBSD management platform for
Bhyve VMs, FreeBSD Jails, and ZFS storage. The plugin communicates with the
Sylve REST API to provision, configure, and snapshot virtual machines directly
on a FreeBSD host running Bhyve.

### Installation

To install this plugin, add the following to your Packer template and run
[`packer init`][docs-packer-init].

```hcl
packer {
  required_plugins {
    sylve = {
      version = ">= 0.1.0"
      source  = "github.com/xoro/sylve"
    }
  }
}
```

Alternatively, you can use `packer plugins install` to manage installation of
this plugin.

```shell
packer plugins install github.com/xoro/sylve
```

### Components

#### Builders

- [`sylve-iso`][docs-sylve-iso] - Creates a Bhyve VM, boots it from an ISO,
  runs provisioners over SSH, and produces an artifact from the resulting VM
  state.
- `sylve-vm` _(planned)_ - Starts from an existing Sylve VM snapshot, runs
  provisioners, and produces an updated artifact.
- `sylve-jail` _(planned)_ - Creates a FreeBSD Jail image via the Sylve API.

#### Provisioners

- `sylve` _(planned)_ - Configures a running Sylve guest.

#### Post-Processors

- `sylve` _(planned)_ - Processes build artifacts after the Packer build.

#### Datasources

- `sylve` _(planned)_ - Queries existing Sylve resources for use in Packer
  templates.

[sylve]: https://github.com/AlchemillaHQ/Sylve
[docs-sylve-iso]: builders/sylve-iso.mdx
[docs-packer-init]: https://developer.hashicorp.com/packer/docs/commands/init
