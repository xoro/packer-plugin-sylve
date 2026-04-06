#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# Run All Quality Checks, Scanners, and Tests
# ===============================================================================
#
# Runs the full local quality pipeline in order: verify formatting (read-only),
# lint, unit tests (with coverage), secret scanning, and security scanners
# (SBOM + vulnerability scanners). Does not apply formatting; run
# bin/format_code.sh separately when you want to auto-fix style. See
# .cursor/rules/test-scripts.mdc for the script inventory.
#
# The script performs the following tasks:
# 1. Verify formatting (bin/run_format_checks.sh)
# 2. Run linters (bin/run_linter_checks.sh)
# 3. Run unit tests with coverage (bin/run_unit_tests.sh)
# 4. Run gitleaks secret scanning (bin/run_leak_checks.sh)
# 5. Run security scanners (bin/run_security_scanners.sh; includes SBOM)
#
# Not included: bin/format_code.sh (writes files), bin/install_git_hooks.sh,
# bin/run_example.sh (live Sylve), bin/update_go_mods.sh (dependency maintenance).
#
# Usage:
#   ./bin/run_all.sh
#
# Dependencies:
#   - Same as each invoked script (treefmt, go, gitleaks, syft, etc.)
#
# Exit Codes:
#   0  - All steps succeeded
#   1  - Could not change to repository root
#   2  - Format checks failed
#   3  - Linters failed
#   4  - Unit tests failed
#   5  - Secret scanning failed
#   6  - Security scanners failed
#
# ===============================================================================

set -eu

script_name="$(basename "${0}")"
repo_root="$(cd "$(dirname "${0}")/.." && pwd)"
cd "${repo_root}" || exit 1

step_text="Verify formatting (read-only)"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! ./bin/run_format_checks.sh; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 2
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Run linters"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! ./bin/run_linter_checks.sh; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 3
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Run unit tests with coverage"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! ./bin/run_unit_tests.sh; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 4
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Run secret scanning (gitleaks)"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! ./bin/run_leak_checks.sh; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 5
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Run security scanners (SBOM + osv-scanner + govulncheck + trivy + semgrep + grype)"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! ./bin/run_security_scanners.sh; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 6
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "All quality checks completed"
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "All quality checks completed"

exit 0
