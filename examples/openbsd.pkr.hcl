// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// OpenBSD sylve-iso source and build.
// Common variables are declared in variables.pkr.hcl in this directory.

// ---------------------------------------------------------------------------
// OpenBSD-specific variable
// ---------------------------------------------------------------------------

variable "openbsd_version" {
  description = "OpenBSD version to install, e.g. 7.8."
  type        = string
  default     = "7.8"
}

// ---------------------------------------------------------------------------
// Locals -- compute ISO URL from version variable
// ---------------------------------------------------------------------------

locals {
  // OpenBSD uses the version number with the dot stripped for directory paths,
  // e.g. version 7.8 lives under /pub/OpenBSD/7.8/ but the ISO is named
  // install78.iso (no dot).
  openbsd_version_nodot = replace(var.openbsd_version, ".", "")
  openbsd_iso_url       = format("https://cdn.openbsd.org/pub/OpenBSD/%s/amd64/install%s.iso", var.openbsd_version, local.openbsd_version_nodot)
}

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

source "sylve-iso" "openbsd" {
  // Connection
  sylve_url       = var.sylve_url
  sylve_token     = var.sylve_token
  sylve_user      = var.sylve_user
  sylve_password  = var.sylve_password
  tls_skip_verify = true

  // ISO -- Sylve's download manager fetches this URL.
  iso_download_url = local.openbsd_iso_url

  // Network -- virtio NIC is exposed as vio0 inside OpenBSD.
  switch_name           = var.switch_name
  switch_emulation_type = "virtio"

  // VM hardware
  vm_name     = "{{build_type}}_{{build_name}}_{{uuid}}"
  cpu_cores   = var.cpu_cores
  ram         = var.ram
  loader      = "uefi"
  time_offset = "utc"

  // Storage -- virtio-blk exposes the disk as sd0 inside OpenBSD.
  storage_size_mb        = var.storage_size_mb
  storage_emulation_type = "virtio-blk"

  // HTTP server -- serves the autoinstall(8) response file.
  //
  // OpenBSD's installer performs an mDNS/DNS lookup for "install.example.com"
  // but more reliably the DHCP server can point it to the HTTP server via
  // the next-server / filename options. In practice Packer's built-in HTTP
  // server is reachable and the boot command below presses <a> to trigger
  // autoinstall mode, which then fetches the response file automatically.
  http_content = {
    "/unattended.conf" = templatefile("${path.root}/data/openbsd/unattended.conf.pkrtpl", {
      ssh_password = var.ssh_password
    })
  }

  // Boot commands
  //
  // After boot_wait the OpenBSD installer shows the boot prompt.
  // Steps:
  //   1. Wait for the boot> prompt, then press Enter to boot the installer.
  //   2. At the installer welcome screen press 'a' for Auto install.
  //   3. The installer tries to configure the network and locate an install.conf
  //      file. We type the Packer HTTP server address when prompted for the
  //      HTTP server location.
  //   4. The installer runs unattended and reboots into the installed OS.
  boot_wait         = var.boot_wait
  boot_key_interval = "100ms"
  boot_command = [
    "autoinstall<return><wait5s>",
    "http://{{ .HTTPIP }}:{{ .HTTPPort }}/unattended.conf<return><wait2s>",
    "install<return><wait160s>",
  ]

  // SSH communicator
  communicator = "ssh"
  ssh_username = "root"
  ssh_password = var.ssh_password
  ssh_timeout  = var.ssh_timeout

  // Lifecycle
  shutdown_command      = "/sbin/halt -p"
  restart_after_install = false
  destroy               = true
  keep_on_error         = var.keep_on_error
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

build {
  sources = ["source.sylve-iso.openbsd"]

  // Upload main.c from the Packer host to the guest.
  provisioner "file" {
    source      = "${path.root}/data/main.c"
    destination = "/tmp/main.c"
  }

  // Compile main.c on the guest.
  // OpenBSD ships cc (clang) in the base system.
  provisioner "shell" {
    inline = ["cc -o /tmp/hello /tmp/main.c"]
  }

  // Download the compiled binary from the guest back to the Packer host.
  provisioner "file" {
    direction   = "download"
    source      = "/tmp/hello"
    destination = "${path.root}/hello-openbsd"
  }
}
