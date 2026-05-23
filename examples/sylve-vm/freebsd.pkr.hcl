// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// FreeBSD sylve-vm example.
// Common variables are declared in variables.pkr.hcl in this directory.
//
// Starts from an existing Sylve-registered FreeBSD VM, installs pkg packages
// via SSH, and shuts down cleanly. The VM state is preserved after the build
// (destroy = false, keep_registered = true).

source "sylve-vm" "freebsd" {
  // Connection
  sylve_url      = var.sylve_url
  sylve_token    = var.sylve_token
  sylve_user     = var.sylve_user
  sylve_password = var.sylve_password

  // Existing VM to use as the build base.
  vm_name = var.vm_name

  // Snapshot all ZFS-backed disks before booting so the original state can be
  // rolled back if the build fails.
  preserve_original = true

  // SSH communicator -- connects once the VM's DHCP lease is visible to Sylve.
  // The VM's DHCP IP is on Sylve's internal bridge; the builder automatically
  // routes SSH through the Sylve host when it is remote (no manual bastion
  // configuration needed).
  communicator = "ssh"
  ssh_username = var.ssh_username
  ssh_password = var.ssh_password
  ssh_timeout  = "5m"

  // Shut down via SSH so the filesystem is cleanly unmounted.
  shutdown_command = "/sbin/shutdown -p now"

  // Leave the VM registered in Sylve after a successful build.
  destroy         = false
  keep_registered = true
}

build {
  sources = ["source.sylve-vm.freebsd"]

  provisioner "shell" {
    inline = [
      "uname -a",
    ]
  }
}
