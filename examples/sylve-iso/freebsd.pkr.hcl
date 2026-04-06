// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// FreeBSD sylve-iso source and build.
// Common variables are declared in variables.pkr.hcl in this directory.

// ---------------------------------------------------------------------------
// FreeBSD-specific variable
// ---------------------------------------------------------------------------

variable "freebsd_version" {
  description = "FreeBSD version to install, e.g. 15.0-RELEASE."
  type        = string
  default     = "15.0-RELEASE"
}

// ---------------------------------------------------------------------------
// Locals -- compute ISO URL from version variable
// ---------------------------------------------------------------------------

locals {
  freebsd_version_short = regex_replace(var.freebsd_version, "-.*", "")
  freebsd_iso_url       = format("https://download.freebsd.org/ftp/releases/ISO-IMAGES/%s/FreeBSD-%s-amd64-dvd1.iso", local.freebsd_version_short, var.freebsd_version)
}

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

source "sylve-iso" "freebsd" {
  // Connection
  sylve_url       = var.sylve_url
  sylve_token     = var.sylve_token
  sylve_user      = var.sylve_user
  sylve_password  = var.sylve_password
  tls_skip_verify = true

  // ISO -- Sylve's download manager fetches this URL.
  iso_download_url = local.freebsd_iso_url

  // Network -- virtio emulation exposes the NIC as vtnet0 inside the guest.
  switch_name           = var.switch_name
  switch_emulation_type = "virtio"

  // VM hardware
  vm_name     = "{{build_type}}_{{build_name}}_{{uuid}}"
  cpu_cores   = var.cpu_cores
  ram         = var.ram
  loader      = "uefi"
  time_offset = "utc"

  // Storage -- virtio-blk exposes the disk as vtbd0 inside the guest.
  storage_size_mb        = var.storage_size_mb
  storage_emulation_type = "virtio-blk"

  // HTTP server -- serves the rendered bsdinstall script.
  http_content = {
    "/unattended.conf" = templatefile("${path.root}/data/freebsd/unattended.conf.pkrtpl", {
      ssh_password = var.ssh_password
    })
  }

  // Boot commands
  //
  // After boot_wait (30s) the FreeBSD installer is at the Welcome dialog, which
  // shows three buttons: [Install] [Shell] [Live CD].
  // <tab> moves from the default "Install" selection to "Shell".
  // <return> activates "Shell", dropping into a root shell.
  //
  // From the shell:
  //   1. Bring up the virtio NIC (vtnet0) via DHCP.
  //   2. Fetch the rendered bsdinstall script from Packer's HTTP server.
  //   3. Run bsdinstall in script mode -- installs base + kernel, configures
  //      SSH, sets the root password, and reboots into the installed OS.
  boot_wait         = "30s"
  boot_key_interval = "100ms"
  boot_command = [
    "<tab><return><wait2s>",
    "dhclient vtnet0<return><wait3s>",
    "fetch --output=/tmp/unattended.conf http://{{ .HTTPIP }}:{{ .HTTPPort }}/unattended.conf<return><wait2s>",
    "bsdinstall script /tmp/unattended.conf<return><wait45s>",
  ]

  // SSH communicator
  communicator = "ssh"
  ssh_username = "root"
  ssh_password = var.ssh_password
  ssh_timeout  = var.ssh_timeout

  // Lifecycle
  shutdown_command      = "/sbin/poweroff 2>&1"
  restart_after_install = true
  destroy               = true
  keep_on_error         = var.keep_on_error
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

build {
  sources = ["source.sylve-iso.freebsd"]

  // Install gcc from the FreeBSD package repository.
  provisioner "shell" {
    inline = ["pkg install --yes gcc"]
  }

  // Upload main.c from the Packer host to the guest.
  provisioner "file" {
    source      = "${path.root}/data/main.c"
    destination = "/tmp/main.c"
  }

  // Compile main.c on the guest.
  provisioner "shell" {
    inline = ["cc -o /tmp/hello /tmp/main.c"]
  }

  // Download the compiled binary from the guest back to the Packer host.
  provisioner "file" {
    direction   = "download"
    source      = "/tmp/hello"
    destination = "${path.root}/hello-freebsd"
  }
}
