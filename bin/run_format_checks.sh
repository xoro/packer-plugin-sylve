#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# packer-plugin-sylve Format Checking Script
# ===============================================================================
#
# This script checks the formatting of all code files in the packer-plugin-sylve
# codebase to ensure consistent formatting across different file types. It
# validates that all files follow the project's formatting standards without
# modifying them.
#
# The script performs the following tasks:
# 1. Environment Information:
#    - Displays version information for all relevant tools
#    - Shows PATH and tool versions for debugging
#
# 2. Unified Format Check (via treefmt):
#    - Runs treefmt --fail-on-change to detect any formatting issues
#    - Configured formatters: Go (gofmt), Shell (shfmt), Packer HCL (packer fmt),
#      TOML (taplo), C (clang-format), Markdown and JSON (prettier)
#    - Uses --fail-on-change flag to detect formatting issues without modifying files
#    - See treefmt.toml for detailed configuration
#
# 3. Makefile Parse Check (GNU make):
#    - Runs make -n build to verify the Makefile parses correctly with GNU make
#
# 4. Makefile Parse Check (bmake):
#    - Runs bmake -n build to verify the Makefile parses correctly with bmake
#
# This script is typically run during CI/CD or before commits to ensure
# all code follows the project's formatting standards.
#
# Usage:
#   ./bin/run_format_checks.sh
#
# Dependencies:
#   - treefmt (unified formatter, see treefmt.toml for per-formatter config)
#   - make (GNU make, for Makefile parse check)
#   - bmake (BSD make, for Makefile parse check)
#
# Exit Codes:
#   0  - All format checks passed successfully
#   1  - treefmt detected formatting issues
#   2  - Makefile failed to parse with GNU make
#   3  - Makefile failed to parse with bmake
#
# ===============================================================================

script_name="$(basename "${0}")"

printf "%b %b DEBUG: go version:            <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(go version | cut -d " " -f 3 | sed 's/^go//')"
printf "%b %b DEBUG: treefmt version:       <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(treefmt --version | cut -d " " -f 2)"
printf "%b %b DEBUG: shfmt version:         <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(shfmt --version)"
printf "%b %b DEBUG: packer version:        <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(packer --version | cut -d v -f 2)"
printf "%b %b DEBUG: taplo version:         <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(taplo --version | cut -d " " -f 2)"
printf "%b %b DEBUG: clang-format version:  <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(clang-format --version | cut -d " " -f 3)"
printf "%b %b DEBUG: prettier version:      <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(prettier --version)"
printf "%b %b DEBUG: make version:          <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(make --version | head -n 1 | cut -d " " -f 3)"
printf "%b %b DEBUG: bmake version:         <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(bmake -V .MAKE.VERSION 2>/dev/null)"

# Treefmt - unified format check
step_text="Run treefmt format check"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! treefmt --fail-on-change --verbose; then
    printf "\n%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    printf "%b %b ERROR: Format inconsistencies detected in the files listed above\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    printf "%b %b INFO:  Run './bin/format_code.sh' to fix formatting issues\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 1
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Makefile parse check - GNU make
step_text="Check Makefile parses correctly with GNU make"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! make -n build >/dev/null 2>&1; then
    printf "\n%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 2
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Makefile parse check - bmake
step_text="Check Makefile parses correctly with bmake"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! bmake -n build >/dev/null 2>&1; then
    printf "\n%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 3
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

exit 0
