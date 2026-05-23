<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# sylve-vm Examples

Packer templates that use the `sylve-vm` builder to provision existing
[Sylve](https://github.com/AlchemillaHQ/Sylve)-registered VMs.

Three OS examples are provided, all sharing the variables declared in
`variables.pkr.hcl`:

- `freebsd.pkr.hcl` — installs packages via SSH, keeps the VM registered
- `linux.pkr.hcl` — updates packages via SSH, destroys the VM after the build
- `windows.pkr.hcl` — runs PowerShell via WinRM, keeps the VM registered

## Prerequisites

- Packer >= 1.15.0
- A running [Sylve](https://github.com/AlchemillaHQ/Sylve) instance
- An existing VM registered in Sylve with SSH (FreeBSD/Linux) or WinRM (Windows) enabled

## Authentication

Choose one method:

**Token-based (recommended):**

```bash
export SYLVE_TOKEN=<your-bearer-token>
```

**Username and password:**

```bash
export SYLVE_USER=admin
export SYLVE_PASSWORD=<your-password>
```

## Usage

```bash
packer init .
packer build -only=sylve-vm.freebsd \
  -var 'sylve_url=https://192.168.1.10:8181' \
  -var 'vm_name=my-freebsd-vm' \
  .
```

For the Windows example:

```bash
packer init .
packer build -only=sylve-vm.windows \
  -var 'sylve_url=https://192.168.1.10:8181' \
  -var 'vm_name=my-windows-vm' \
  -var 'winrm_password=MyPassword123' \
  .
```

### WinRM routing note

WinRM traffic is tunnelled automatically through the Sylve host over SSH when
the Packer host is not the Sylve host, exactly like the SSH auto-bastion. No
manual configuration is needed. SSH auth uses the same key resolution order:
`SYLVE_SSH_PROXY_KEY` env var → `~/.ssh/config` IdentityFile → default key
files → SSH agent.

Set `winrm_host` explicitly in HCL to bypass the tunnel and manage routing
yourself.
