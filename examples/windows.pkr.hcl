// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Windows Server sylve-vm example (WinRM communicator).
// Common variables are declared in variables.pkr.hcl in this directory.
//
// Creates a new VM by cloning a Sylve template, runs PowerShell provisioners
// via WinRM, and shuts down gracefully.
//
// Prerequisites on the Windows template (run once in an elevated PowerShell):
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
//     -var 'winrm_password=MyPassword123' \
//     .

// ---------------------------------------------------------------------------
// Windows-specific variables
// ---------------------------------------------------------------------------

variable "windows_source_template" {
  description = "Name of the Sylve template to clone the Windows VM from."
  type        = string
  default     = "packer-windows11pro.localdomain"
}

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

source "sylve-vm" "windows" {
  // Connection
  sylve_url      = var.sylve_url
  sylve_token    = var.sylve_token
  sylve_user     = var.sylve_user
  sylve_password = var.sylve_password

  // Template to clone from and name for the new VM.
  source_template = var.windows_source_template
  vm_name         = "{{build_type}}_{{build_name}}_{{uuid}}"

  // Wait for Windows to boot and WinRM to become available.
  boot_wait = "1m"

  // WinRM communicator -- connects once the VM's DHCP lease is visible to Sylve.
  communicator   = "winrm"
  winrm_username = var.winrm_username
  winrm_password = var.winrm_password
  winrm_port     = var.winrm_port
  winrm_timeout  = var.winrm_timeout

  // Graceful Windows shutdown via WinRM.
  shutdown_command = "shutdown /s /f /t 5"

  // Delete the cloned VM after a successful build.
  keep_registered = false
}

// ---------------------------------------------------------------------------
// Build
// ---------------------------------------------------------------------------

build {
  sources = ["source.sylve-vm.windows"]

  provisioner "file" {
    source      = "data/main.c"
    destination = "C:\\Users\\${var.winrm_username}\\main.c"
  }

  provisioner "powershell" {
    inline = [
      "[System.Environment]::OSVersion.VersionString",
      "(Get-ComputerInfo).WindowsProductName",

      // Install TCC (Tiny C Compiler) — single zip, no installer needed.
      // Discover the latest win64-bin zip from the Savannah release index.
      "$page = (Invoke-WebRequest -Uri 'http://download.savannah.gnu.org/releases/tinycc/' -UseBasicParsing).Content; $zip = [regex]::Matches($page, 'tcc-[0-9.]+-win64-bin\\.zip') | Select-Object -Last 1 -ExpandProperty Value; $url = \"http://download.savannah.gnu.org/releases/tinycc/$zip\"; Write-Output \"Downloading $url\"; Invoke-WebRequest -Uri $url -OutFile $env:USERPROFILE\\tcc.zip",
      "Expand-Archive -Path $env:USERPROFILE\\tcc.zip -DestinationPath $env:USERPROFILE\\tcc",

      // Compile and run main.c.
      "& $env:USERPROFILE\\tcc\\tcc\\tcc.exe -o $env:USERPROFILE\\main.exe $env:USERPROFILE\\main.c",
      "& $env:USERPROFILE\\main.exe",

      "Write-Output 'Packer provisioner ran successfully'",
    ]
  }

  // Download the compiled binary from the guest back to the Packer host.
  provisioner "file" {
    direction   = "download"
    source      = "C:\\Users\\${var.winrm_username}\\main.exe"
    destination = "${path.root}/hello-windows.exe"
  }
}
