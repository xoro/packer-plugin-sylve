// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Linux (Debian/Ubuntu) sylve-vm example.
// Common variables are declared in variables.pkr.hcl in this directory.
//
// Starts from an existing Sylve-registered Linux VM, runs apt package
// updates via SSH, then destroys the VM after a successful build.

source "sylve-vm" "linux" {
  // Connection
  sylve_url      = var.sylve_url
  sylve_token    = var.sylve_token
  sylve_user     = var.sylve_user
  sylve_password = var.sylve_password

  // Existing VM to use as the build base.
  vm_name = var.vm_name

  // Do not snapshot -- the VM will be destroyed after the build.
  preserve_original = false

  // SSH communicator
  communicator = "ssh"
  ssh_username = var.ssh_username
  ssh_password = var.ssh_password
  ssh_timeout  = "5m"

  // Shut down via SSH before the builder tears down the VM.
  shutdown_command = "sudo /sbin/poweroff"

  // Destroy the VM (and all its disks) after the build.
  destroy = true
}

build {
  sources = ["source.sylve-vm.linux"]

  provisioner "shell" {
    execute_command = "sudo bash -c '{{ .Vars }} {{ .Path }}'"
    inline = [
      "export DEBIAN_FRONTEND=noninteractive",
      "apt-get update -y",
      "apt-get upgrade -y",
      "apt-get install -y curl wget",
    ]
  }
}
