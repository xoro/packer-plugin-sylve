#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# packer-plugin-sylve Linter Checks Script
# ===============================================================================
#
# This script runs linters and static analysis tools on the packer-plugin-sylve
# codebase to ensure code quality and identify potential issues.
#
# The script performs the following tasks:
# 1. Go Static Analysis:
#    - Runs staticcheck for Go static analysis and bug detection
#
# 2. Shell Script Linting:
#    - Runs shellcheck to validate POSIX compliance and best practices
#    - Targets all scripts in bin/
#
# 3. HCL Linting:
#    - Runs packer validate -syntax-only on all .pkr.hcl files in examples/
#
# 4. TOML Linting:
#    - Runs taplo lint on all .toml files
#
# 5. JSON Linting:
#    - Runs jq empty to validate all config JSON files (excludes generated artifacts)
#
# 6. C Code Linting:
#    - Runs cppcheck for static analysis on C source files in examples/
#
# 7. Markdown Linting:
#    - Runs markdownlint to check Markdown file quality and style
#
# 7. Documentation Link Checking:
#    - Runs lychee to check for broken links in Markdown files
#
# This script is typically run during CI/CD or before commits to ensure
# code quality and identify potential issues early.
#
# Usage:
#   ./bin/run_linter_checks.sh
#
# Dependencies:
#   - staticcheck (for Go static analysis)
#   - shellcheck (for shell scripts)
#   - packer (for HCL syntax validation)
#   - taplo (for TOML linting)
#   - jq (for JSON validation)
#   - cppcheck (for C source files)
#   - markdownlint (for Markdown files)
#   - lychee (for link checking)
#
# Exit Codes:
#   0  - All linter checks passed successfully
#   1  - Failed to run staticcheck
#   2  - Failed to run shellcheck
#   3  - Failed to run packer validate (HCL)
#   4  - Failed to run taplo lint (TOML)
#   5  - Failed to run jq (JSON)
#   6  - Failed to run cppcheck (C)
#   7  - Failed to run markdownlint
#   8  - Failed to run lychee
#
# ===============================================================================

script_name="$(basename "${0}")"

printf "%b %b DEBUG: go version:           <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(go version | cut -d " " -f 3 | sed 's/^go//')"
printf "%b %b DEBUG: staticcheck version:  <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(staticcheck --version 2>&1 | cut -d " " -f 2)"
printf "%b %b DEBUG: shellcheck version:   <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(shellcheck --version | grep ^version | cut -d " " -f 2)"
printf "%b %b DEBUG: packer version:       <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(packer --version | cut -d v -f 2)"
printf "%b %b DEBUG: taplo version:        <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(taplo --version | cut -d " " -f 2)"
printf "%b %b DEBUG: jq version:           <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(jq --version | cut -d "-" -f 2)"
printf "%b %b DEBUG: cppcheck version:     <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(cppcheck --version | cut -d " " -f 2)"
printf "%b %b DEBUG: markdownlint version: <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(markdownlint --version)"
printf "%b %b DEBUG: lychee version:       <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(lychee --version | cut -d " " -f 2)"

# Go static analysis
step_text="Run staticcheck Go static analysis"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! staticcheck ./...; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 1
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Shell script linting
step_text="Run shellcheck on shell scripts"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! shellcheck --shell=sh bin/*.sh; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 2
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# HCL syntax validation
step_text="Run packer validate on HCL files"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
find examples/ -name '*.pkr.hcl' | while IFS= read -r hcl_file; do
    if ! packer validate -syntax-only "${hcl_file}"; then
        printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
        exit 3
    fi
done
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# TOML linting
step_text="Run taplo lint on TOML files"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! taplo lint; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 4
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# JSON validation
step_text="Validate JSON config files"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
find . -not -path './.git/*' -name '*.json' ! -name 'grype_*.json' ! -name 'osv-scanner_*.json' | while IFS= read -r json_file; do
    if ! jq empty "${json_file}" 2>&1; then
        printf "%b %b ERROR: Invalid JSON in: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${json_file}"
        printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
        exit 5
    fi
done
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# C static analysis
step_text="Run cppcheck on C source files"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! find examples/ -name '*.c' -exec cppcheck --enable=all --suppress=missingIncludeSystem --error-exitcode=1 {} +; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 6
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Markdown linting
step_text="Run markdownlint on Markdown files"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! markdownlint "**/*.md"; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 7
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Documentation link checking
step_text="Run lychee link checker on Markdown files"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! lychee --offline "**/*.md"; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 8
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

exit 0
