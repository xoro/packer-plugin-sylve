#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# packer-plugin-sylve Run Example Script
# ===============================================================================
#
# This script builds and installs the packer-plugin-sylve binary, then runs
# the Packer example for the specified operating system. It ensures the locally
# built plugin is used instead of any previously installed version.
#
# The script performs the following tasks:
# 1. Validates the required OS argument.
# 2. Validates that the example directory exists for the specified OS.
# 3. Builds the packer-plugin-sylve binary from source.
# 4. Installs the binary into the Packer plugin cache for local use.
# 5. Runs packer init and packer build in the example directory.
#
# Usage:
#   ./bin/run_example.sh <os-name>
#
# Examples:
#   ./bin/run_example.sh alpine
#
# Arguments:
#   os-name  Name of the OS example to run. Must match a directory under
#            examples/sylve-iso/<os-name>/ containing a *.pkr.hcl template.
#
# Environment Variables:
#   SYLVE_URL      Full base URL of the Sylve instance, e.g. https://host:8181.
#                  Takes precedence over SYLVE_HOST.
#   SYLVE_HOST     Hostname of the Sylve instance. Used to construct the URL
#                  https://<SYLVE_HOST>:8181 when SYLVE_URL is not set.
#   SYLVE_TOKEN    Pre-issued Bearer token for the Sylve API.
#   SYLVE_USER     Sylve account username (alternative to SYLVE_TOKEN).
#   SYLVE_PASSWORD Sylve account password (alternative to SYLVE_TOKEN).
#   SYLVE_SWITCH   Name of the DHCP-enabled Sylve virtual switch (required).
#   SWITCH_NAME    Alias for SYLVE_SWITCH (SYLVE_SWITCH takes precedence).
#   PACKER_LOG_PATH  Path for the Packer debug log (default: packer.log in the
#                    repository root). Debug logging is always enabled.
#
# Dependencies:
#   - go (for building the plugin)
#   - packer (>= 1.15.0)
#
# Exit Codes:
#   0 - Build and example run succeeded.
#   1 - Missing or invalid argument.
#   2 - Example directory not found.
#   3 - Plugin build failed.
#   4 - packer init failed.
#   5 - Plugin installation failed.
#   6 - packer build failed.
#
# ===============================================================================

set -eu

script_name="$(basename "${0}")"
repo_root="$(cd "$(dirname "${0}")/.." && pwd)"

# ---------------------------------------------------------------------------
# Argument validation
# ---------------------------------------------------------------------------

if [ "${#}" -ne 1 ]; then
    printf "%b %b ERROR: ==>> Usage: %b <os-name>\n" \
        "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${script_name}"
    exit 1
fi

os_name="${1}"
example_dir="${repo_root}/examples/sylve-iso"

# Always enable Packer debug logging; write to a file so the build output
# is not interleaved with the verbose log on stdout.
PACKER_LOG=1
export PACKER_LOG
PACKER_LOG_PATH="${PACKER_LOG_PATH:-${repo_root}/packer.log}"
export PACKER_LOG_PATH
printf "%b %b DEBUG: PACKER_LOG_PATH=<%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${PACKER_LOG_PATH}"

if [ ! -f "${example_dir}/${os_name}.pkr.hcl" ]; then
    printf "%b %b ERROR: ==>> No example found for OS: %b (expected %b/%b.pkr.hcl)\n" \
        "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${os_name}" "${example_dir}" "${os_name}"
    exit 2
fi

# ---------------------------------------------------------------------------
# Step 1: Build the plugin binary
# ---------------------------------------------------------------------------

step_text="Build packer-plugin-sylve binary"
printf "\n%b %b INFO:  ==>> STEP: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
cd "${repo_root}"
plugin_fqn="$(grep -E '^module' go.mod | sed 's/module[[:space:]]*//')"
if ! go build -ldflags="-X '${plugin_fqn}/version.VersionPrerelease='" -o packer-plugin-sylve .; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 3
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# ---------------------------------------------------------------------------
# Step 2: packer init (downloads any missing plugins from the registry)
# ---------------------------------------------------------------------------

step_text="packer init examples/sylve-iso"
printf "\n%b %b INFO:  ==>> STEP: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
cd "${example_dir}"
if ! packer init .; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 4
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
cd "${repo_root}"

# ---------------------------------------------------------------------------
# Step 3: Install the dev plugin binary into the Packer plugin cache
#         (overwrites whatever packer init downloaded above)
# ---------------------------------------------------------------------------

step_text="Install plugin into Packer plugin cache"
printf "\n%b %b INFO:  ==>> STEP: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! packer plugins install --path "${repo_root}/packer-plugin-sylve" "github.com/xoro/sylve"; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 5
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# ---------------------------------------------------------------------------
# Step 4: packer build
# ---------------------------------------------------------------------------

step_text="packer build ${os_name}"
printf "\n%b %b INFO:  ==>> STEP: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
cd "${example_dir}"

# Resolve Sylve base URL: SYLVE_URL takes precedence; fall back to SYLVE_HOST
resolved_url="${SYLVE_URL:-}"
if [ -z "${resolved_url}" ] && [ -n "${SYLVE_HOST:-}" ]; then
    resolved_url="https://${SYLVE_HOST}:8181"
fi
printf "%b %b DEBUG: sylve_url=<%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${resolved_url}"

build_args=""
if [ -n "${resolved_url}" ]; then
    build_args="${build_args} -var sylve_url=${resolved_url}"
fi
# Accept SYLVE_SWITCH (preferred) or SWITCH_NAME (alias)
resolved_switch="${SYLVE_SWITCH:-${SWITCH_NAME:-}}"
if [ -n "${resolved_switch}" ]; then
    build_args="${build_args} -var switch_name=${resolved_switch}"
fi

build_args="${build_args} -only=sylve-iso.${os_name}"

# shellcheck disable=SC2086
if ! packer build ${build_args} .; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 6
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
