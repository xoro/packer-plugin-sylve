// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Debian sylve-iso source and build.
// Common variables are declared in variables.pkr.hcl in this directory.

// ---------------------------------------------------------------------------
// Debian-specific variable
// ---------------------------------------------------------------------------

variable "debian_version" {
  description = "Debian version to install, e.g. 13.4.0."
  type        = string
  default     = "13.4.0"
}

// ---------------------------------------------------------------------------
// Locals -- compute ISO URL from version variable
// ---------------------------------------------------------------------------

locals {
  debian_iso_url = format("https://cdimage.debian.org/cdimage/release/%s/amd64/iso-dvd/debian-%s-amd64-DVD-1.iso", var.debian_version, var.debian_version)
}

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

source "sylve-iso" "debian" {
  // Connection
  sylve_url       = var.sylve_url
  sylve_token     = var.sylve_token
  sylve_user      = var.sylve_user
  sylve_password  = var.sylve_password
  tls_skip_verify = true

  // ISO -- Sylve's download manager fetches this URL.
  iso_download_url = local.debian_iso_url

  // Network -- virtio NIC attaches to the named DHCP switch.
  switch_name           = var.switch_name
  switch_emulation_type = "virtio"

  // VM hardware
  vm_name     = "{{build_type}}_{{build_name}}_{{uuid}}"
  cpu_cores   = var.cpu_cores
  ram         = var.ram
  loader      = "uefi"
  time_offset = "utc"

  // Storage -- virtio-blk exposes the disk as /dev/vda inside the guest.
  storage_size_mb        = var.storage_size_mb
  storage_emulation_type = "virtio-blk"

  // HTTP server -- serves the rendered preseed file.
  // The preseed URL is passed to the Debian installer via a boot command.
  http_content = {
    "/preseed.cfg" = templatefile("${path.root}/data/debian/preseed.cfg.pkrtpl", {
      ssh_password = var.ssh_password
    })
  }

  // Boot commands
  //
  // The Debian DVD boot menu has "Graphical install" selected by default.
  // Two <down> presses select "Advanced options", then five more select
  // "Automated install". The d-i boot prompt then accepts the preseed URL.
  boot_wait         = var.boot_wait
  boot_key_interval = "100ms"
  boot_command = [
    "<down><down><return><wait3s>",
    "<down><down><down><down><down><return><wait45s>",
    "http://{{ .HTTPIP }}:{{ .HTTPPort }}/preseed.cfg<tab><return><wait230s>",
  ]

  // SSH communicator
  communicator = "ssh"
  ssh_username = "root"
  ssh_password = var.ssh_password
  ssh_timeout  = var.ssh_timeout

  // Lifecycle
  shutdown_command      = "/usr/sbin/poweroff"
  restart_after_install = false
  destroy               = true
  keep_on_error         = var.keep_on_error
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

build {
  sources = ["source.sylve-iso.debian"]

  // Install gcc to compile the example C program.
  provisioner "shell" {
    inline = ["apt-get install -y gcc"]
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
    destination = "${path.root}/hello-debian"
  }
}
