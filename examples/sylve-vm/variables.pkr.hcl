// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Shared plugin requirement and variable declarations for all sylve-vm examples.
// Both freebsd.pkr.hcl and linux.pkr.hcl in this directory consume these variables.
//
// Run a specific OS example:
//   packer init .
//   packer build -only=sylve-vm.<os> \
//     -var 'vm_name=my-existing-vm' \
//     .
//
// Connection (choose one):
//   SYLVE_HOST env var          -- hostname only; plugin constructs https://<host>:8181
//   -var 'sylve_url=https://...' -- full URL override
//
// Authentication (choose one):
//   SYLVE_TOKEN env var         -- pre-issued Bearer token (recommended)
//   SYLVE_USER + SYLVE_PASSWORD -- login-based auth

packer {
  required_version = ">= 1.15.0"
  required_plugins {
    sylve = {
      version = ">= 0.1.0"
      source  = "github.com/xoro/sylve"
    }
  }
}

// ---------------------------------------------------------------------------
// Sylve connection
// ---------------------------------------------------------------------------

variable "sylve_url" {
  description = "Base URL of the Sylve instance. Leave empty to use SYLVE_HOST env var (plugin constructs https://<host>:8181)."
  type        = string
  default     = ""
}

variable "sylve_token" {
  description = "Pre-issued Bearer token for the Sylve API. Falls back to SYLVE_TOKEN env var."
  type        = string
  sensitive   = true
  default     = ""
}

variable "sylve_user" {
  description = "Sylve account username for login-based auth. Falls back to SYLVE_USER env var."
  type        = string
  default     = ""
}

variable "sylve_password" {
  description = "Sylve account password for login-based auth. Falls back to SYLVE_PASSWORD env var."
  type        = string
  sensitive   = true
  default     = ""
}

// ---------------------------------------------------------------------------
// VM selection
// ---------------------------------------------------------------------------

variable "vm_name" {
  description = "Name of the EXISTING VM registered in Sylve to use as the build base."
  type        = string
}

// ---------------------------------------------------------------------------
// Communicator
// ---------------------------------------------------------------------------

variable "ssh_username" {
  description = "SSH username used by Packer to connect to the guest."
  type        = string
  default     = "root"
}

variable "ssh_password" {
  description = "SSH password used by Packer to connect to the guest."
  type        = string
  sensitive   = true
  default     = "root"
}

// ---------------------------------------------------------------------------
// SSH bastion / jump host
// ---------------------------------------------------------------------------
// The DHCP IP discovered by the plugin (e.g. 10.200.0.x) is on Sylve's
// internal bridge and is not routable from the Packer host. Set these
// variables to route SSH through the Sylve host itself.

variable "ssh_bastion_host" {
  description = "Hostname or IP of the SSH bastion (jump host) used to reach the VM's internal IP. Typically the Sylve host."
  type        = string
  default     = ""
}

variable "ssh_bastion_username" {
  description = "SSH username on the bastion host. Leave empty to use ~/.ssh/config User or $USER."
  type        = string
  default     = ""
}

variable "ssh_bastion_password" {
  description = "SSH password on the bastion host."
  type        = string
  sensitive   = true
  default     = ""
}

// ---------------------------------------------------------------------------
// WinRM communicator
// ---------------------------------------------------------------------------
// Used by windows.pkr.hcl. When the Packer host cannot reach the VM's DHCP IP
// directly, the builder automatically tunnels WinRM through the Sylve host over
// SSH (same mechanism as the SSH auto-bastion). No extra variables are needed.
// See the WinRM routing note in windows.pkr.hcl for guest setup requirements.

variable "winrm_username" {
  description = "WinRM username used by Packer to connect to the Windows guest."
  type        = string
  default     = "Administrator"
}

variable "winrm_password" {
  description = "WinRM password used by Packer to connect to the Windows guest."
  type        = string
  sensitive   = true
  default     = ""
}

variable "winrm_port" {
  description = "WinRM TCP port on the guest. 5985 = HTTP (default), 5986 = HTTPS (set winrm_use_ssl = true)."
  type        = number
  default     = 5985
}
