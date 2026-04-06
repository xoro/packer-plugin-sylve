#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# packer-plugin-sylve Secret Leak Detection Script
# ===============================================================================
#
# This script scans the packer-plugin-sylve repository for leaked secrets,
# credentials, and other sensitive data using gitleaks.
#
# The script performs the following tasks:
# 1. Secret Detection:
#    - Runs gitleaks to scan the git repository history and working tree
#    - Detects API keys, passwords, tokens, and other secrets
#
# This script is typically run during CI/CD or before commits to ensure
# no sensitive data is accidentally committed.
#
# Usage:
#   ./bin/run_leak_checks.sh
#
# Dependencies:
#   - gitleaks (for secret detection)
#
# Exit Codes:
#   0  - No secrets detected
#   1  - Failed to run gitleaks or secrets detected
#
# ===============================================================================

script_name="$(basename "${0}")"

printf "%b %b DEBUG: gitleaks version:    <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(gitleaks version)"

# Secret detection
step_text="Run gitleaks secret detection"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! gitleaks dir . --no-banner; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 1
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

exit 0
