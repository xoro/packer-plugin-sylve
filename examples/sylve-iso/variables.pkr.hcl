// SPDX-License-Identifier: BSD-2-Clause
// Copyright (c) 2026, Timo Pallach (timo@pallach.de).

// Shared plugin requirement and variable declarations for all sylve-iso examples.
// Both alpine.pkr.hcl and debian.pkr.hcl in this directory consume these variables.
//
// Run a specific OS example:
//   packer init .
//   packer build -only=sylve-iso.<os> \
//     -var 'sylve_url=https://192.168.1.10:8181' \
//     -var 'switch_name=packer-switch' \
//     .
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
  description = "Base URL of the Sylve instance, e.g. https://192.168.1.10:8181."
  type        = string
  default     = "https://localhost:8181"
}

variable "sylve_token" {
  description = "Pre-issued Bearer token for the Sylve API. Falls back to SYLVE_TOKEN env var."
  type        = string
  default     = env("SYLVE_TOKEN")
  sensitive   = true
}

variable "sylve_user" {
  description = "Sylve account username for login-based auth. Falls back to SYLVE_USER env var."
  type        = string
  default     = env("SYLVE_USER")
}

variable "sylve_password" {
  description = "Sylve account password for login-based auth. Falls back to SYLVE_PASSWORD env var."
  type        = string
  default     = env("SYLVE_PASSWORD")
  sensitive   = true
}

// ---------------------------------------------------------------------------
// Network
// ---------------------------------------------------------------------------

variable "switch_name" {
  description = "Name of a DHCP-enabled Sylve virtual switch to attach to the VM."
  type        = string
}

// ---------------------------------------------------------------------------
// VM hardware
// ---------------------------------------------------------------------------

variable "cpu_cores" {
  description = "Number of vCPU cores."
  type        = number
  default     = 2
}

variable "ram" {
  description = "Amount of memory in MiB."
  type        = number
  default     = 2048
}

variable "storage_size_mb" {
  description = "Install disk size in MiB."
  type        = number
  default     = 20480
}

// ---------------------------------------------------------------------------
// Authentication / provisioning
// ---------------------------------------------------------------------------

# Default is not the literal "packer": Packer redacts sensitive values in
# PACKER_LOG, which would otherwise replace "packer" inside packer-plugin-*.
variable "ssh_password" {
  description = "Root password set on the installed system. Used by provisioners."
  type        = string
  default     = "sylve-example"
  sensitive   = true
}

// ---------------------------------------------------------------------------
// Build timing
// ---------------------------------------------------------------------------

variable "boot_wait" {
  description = "Duration to wait after the VM starts before sending VNC boot commands."
  type        = string
  default     = "30s"
}

variable "ssh_timeout" {
  description = "Maximum time to wait for SSH after the installed system reboots."
  type        = string
  default     = "1m"
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

variable "keep_on_error" {
  description = "Keep the VM alive when the build fails, for post-failure debugging."
  type        = bool
  default     = false
}
