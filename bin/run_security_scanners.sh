#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# packer-plugin-sylve Security Scanners Script
# ===============================================================================
#
# This script runs security scanners on the packer-plugin-sylve codebase to
# identify potential vulnerabilities, secrets, and dependency issues.
#
# The script performs the following tasks:
# 1. SBOM Creation:
#    - Runs create_sbom.sh to generate sbom.cdx.xml from the Go module
#
# 2. Dependency Vulnerability Scanning:
#    - Runs osv-scanner against go.mod and sbom.cdx.xml for known CVEs
#    - Uses osv-scanner.toml to ignore unfixable transitive dep CVEs
#    - Runs govulncheck (Go vulnerability database) on the module source
#
# 3. Filesystem Vulnerability Scanning:
#    - Runs trivy to scan for vulnerabilities, misconfigs, and secrets
#
# 4. Static Security Analysis:
#    - Runs semgrep with the auto ruleset to detect security issues in Go code
#
# 5. SBOM Vulnerability Scanning:
#    - Runs grype against the generated SBOM for known CVEs
#
# Usage:
#   ./bin/run_security_scanners.sh
#
# Dependencies:
#   - syft (for SBOM generation, via create_sbom.sh)
#   - xmllint (for SBOM formatting, via create_sbom.sh)
#   - go (for building the plugin binary, via create_sbom.sh)
#   - osv-scanner (for dependency vulnerability scanning)
#   - govulncheck (for Go official vulnerability database scan)
#   - trivy (for filesystem vulnerability scanning)
#   - semgrep (for static security analysis)
#   - grype (for SBOM vulnerability scanning)
#
# Exit Codes:
#   0  - All security scans passed successfully
#   1  - Failed to create SBOM (create_sbom.sh failed)
#   2  - Failed to run osv-scanner
#   3  - Failed to run govulncheck
#   4  - Failed to run trivy
#   5  - Failed to run semgrep
#   6  - Failed to run grype SBOM vulnerability scan
#
# ===============================================================================

script_name="$(basename "${0}")"

printf "%b %b DEBUG: go version:          <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(go version | cut -d " " -f 3 | sed 's/^go//')"
printf "%b %b DEBUG: osv-scanner version: <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(osv-scanner --version 2>&1 | head -1 | cut -d ' ' -f 3)"
printf "%b %b DEBUG: govulncheck version: <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(govulncheck -version 2>&1 | sed -n 's/^Scanner: govulncheck@v//p')"
printf "%b %b DEBUG: trivy version:       <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(trivy --version | head -1 | cut -d ' ' -f 2)"
printf "%b %b DEBUG: semgrep version:     <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(semgrep --version 2>&1 | head -1)"
printf "%b %b DEBUG: grype version:       <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(grype --version 2>&1 | head -1 | cut -d ' ' -f 2)"

# SBOM creation
step_text="Create SBOM"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! bin/create_sbom.sh; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 1
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Dependency vulnerability scanning
step_text="Run osv-scanner dependency vulnerability scan"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! osv-scanner scan --config osv-scanner.toml --recursive .; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 2
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Go official vulnerability database (complements osv-scanner)
step_text="Run govulncheck Go vulnerability scan"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! govulncheck ./...; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 3
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Filesystem vulnerability scanning
step_text="Run trivy filesystem vulnerability scan"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! trivy filesystem --scanners vuln,secret,misconfig .; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 4
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Static security analysis
step_text="Run semgrep static security analysis"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! semgrep --error --config auto .; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 5
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# SBOM vulnerability scanning
step_text="Run grype SBOM vulnerability scan"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
plugin_version=$(sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' version/version.go | head -1)
if ! grype --output json --file "grype_packer-plugin-sylve-${plugin_version}.json" "sbom:./sbom.cdx.xml"; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 6
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

exit 0
