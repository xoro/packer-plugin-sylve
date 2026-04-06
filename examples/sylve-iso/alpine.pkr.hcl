// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Alpine Linux sylve-iso source and build.
// Common variables are declared in variables.pkr.hcl in this directory.

// ---------------------------------------------------------------------------
// Alpine-specific variable
// ---------------------------------------------------------------------------

variable "alpine_version" {
  description = "Alpine Linux version to install, e.g. 3.21.3."
  type        = string
  default     = "3.21.3"
}

// ---------------------------------------------------------------------------
// Locals -- compute ISO URL from version variable
// ---------------------------------------------------------------------------

locals {
  alpine_minor_version = regex_replace(var.alpine_version, "\\.\\d+$", "")
  alpine_iso_url       = format("https://dl-cdn.alpinelinux.org/alpine/v%s/releases/x86_64/alpine-virt-%s-x86_64.iso", local.alpine_minor_version, var.alpine_version)
}

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

source "sylve-iso" "alpine" {
  // Connection
  sylve_url       = var.sylve_url
  sylve_token     = var.sylve_token
  sylve_user      = var.sylve_user
  sylve_password  = var.sylve_password
  tls_skip_verify = true

  // ISO -- Sylve's download manager fetches this URL.
  iso_download_url = local.alpine_iso_url

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

  // HTTP server -- serves the unattended install script rendered with the
  // ssh_password variable so the installed system is accessible via SSH.
  http_content = {
    "/answerfile.sh" = templatefile("${path.root}/data/alpine/answerfile.sh.pkrtpl", {
      ssh_password = var.ssh_password
    })
  }

  // Boot commands
  //
  // After boot_wait the Alpine live system is at a root login prompt.
  // The commands below:
  //   1. Log in as root (no password on the live image).
  //   2. Bring up eth0 and obtain a DHCP lease.
  //   3. Download the rendered unattended install script from Packer's HTTP server.
  //   4. Run it -- the script calls setup-alpine, sets the root password, and
  //      reboots into the installed OS.
  boot_wait         = var.boot_wait
  boot_key_interval = "100ms"
  boot_command = [
    "root<return><wait2s>",
    "ifconfig eth0 up && udhcpc -i eth0<return><wait3s>",
    "wget http://{{ .HTTPIP }}:{{ .HTTPPort }}/answerfile.sh<return><wait2s>",
    "sh answerfile.sh<return><wait90s>",
  ]

  // SSH communicator
  communicator = "ssh"
  ssh_username = "root"
  ssh_password = var.ssh_password
  ssh_timeout  = var.ssh_timeout

  // Lifecycle
  shutdown_command      = "/sbin/poweroff"
  restart_after_install = true
  destroy               = true
  keep_on_error         = var.keep_on_error
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

build {
  sources = ["source.sylve-iso.alpine"]

  // Install the C compiler on the guest.
  provisioner "shell" {
    inline = ["apk add --no-cache gcc musl-dev"]
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
    destination = "${path.root}/hello-alpine"
  }
}
