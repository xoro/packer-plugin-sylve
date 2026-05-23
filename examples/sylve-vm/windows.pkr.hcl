// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Windows Server sylve-vm example (WinRM communicator).
// Common variables are declared in variables.pkr.hcl in this directory.
//
// Starts from an existing Sylve-registered Windows Server VM, runs
// PowerShell provisioners via WinRM, and shuts down via the Sylve API.
// preserve_original = true so the disk is rolled back after the build.
//
// Prerequisites on the Windows guest (run once in an elevated PowerShell):
//   winrm quickconfig -q
//   winrm set winrm/config/service '@{AllowUnencrypted="true"}'
//   winrm set winrm/config/service/auth '@{Basic="true"}'
//   netsh advfirewall firewall add rule name="WinRM HTTP" protocol=TCP dir=in localport=5985 action=allow
//
// WinRM routing is handled automatically: when the Packer host is not the
// Sylve host, the builder opens an SSH port-forward from a random localhost
// port to the VM's WinRM port (5985) through the Sylve host. No manual
// configuration is needed. SSH auth uses the same key resolution as the SSH
// auto-bastion (SYLVE_SSH_PROXY_KEY env var, ~/.ssh/config, default key
// files, or SSH agent).
//
// If you want to manage routing yourself, set winrm_host explicitly in HCL
// or via -var and the tunnel is skipped entirely.
//
// Run this example:
//   packer init .
//   packer build -only=sylve-vm.windows \
//     -var 'vm_name=my-windows-vm' \
//     -var 'winrm_password=MyPassword123' \
//     .

source "sylve-vm" "windows" {
  // Connection
  sylve_url      = var.sylve_url
  sylve_token    = var.sylve_token
  sylve_user     = var.sylve_user
  sylve_password = var.sylve_password

  // Existing VM to use as the build base.
  vm_name = var.vm_name

  // Snapshot all ZFS-backed disks before booting so the original state can
  // be rolled back after the build (success or failure).
  preserve_original = true

  // WinRM communicator -- connects once the VM's DHCP lease is visible to Sylve.
  // The VM IP is on Sylve's internal bridge; see the routing note above.
  communicator   = "winrm"
  winrm_username = var.winrm_username
  winrm_password = var.winrm_password
  winrm_port     = var.winrm_port
  winrm_timeout  = "3m"
  boot_wait      = "1m"

  // Graceful Windows shutdown via WinRM before Sylve stops the VM.
  // This ensures the disk is in a clean-shutdown state when the pre-build
  // snapshot is rolled back, so the next run boots without dirty-shutdown
  // recovery (chkdsk etc.) and WinRM starts promptly.
  shutdown_command = "shutdown /s /f /t 5"

  // Leave the VM registered in Sylve after the build.
  destroy         = false
  keep_registered = true
}

build {
  sources = ["source.sylve-vm.windows"]

  provisioner "powershell" {
    inline = [
      "[System.Environment]::OSVersion.VersionString",
      "(Get-ComputerInfo).WindowsProductName",

      # Rollback check: if the sentinel from the previous build still exists,
      # the snapshot rollback did not work.
      "if (Test-Path 'C:\\packer-sentinel.txt') { throw 'ROLLBACK CHECK FAILED: sentinel from previous build still exists' }",
      "Write-Output 'ROLLBACK CHECK PASSED: no sentinel from previous build'",

      # Create sentinel to prove this build's provisioner ran.
      # The snapshot rollback at the end of the build will remove this file.
      "New-Item -Path 'C:\\packer-sentinel.txt' -ItemType File -Value \"packer-build-$(Get-Date -Format yyyyMMdd-HHmmss)\" -Force | Out-Null",
      "if (Test-Path 'C:\\packer-sentinel.txt') { Write-Output 'SENTINEL CREATED: rollback will remove this file' } else { throw 'SENTINEL CREATION FAILED' }",
    ]
  }
}
