#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# packer-plugin-sylve SBOM Creation Script
# ===============================================================================
#
# This script creates a Software Bill of Materials (SBOM) for the
# packer-plugin-sylve plugin in CycloneDX XML format. It is called by
# run_security_scanners.sh before the SBOM-based security checks are run.
#
# The script performs the following tasks:
# 1. Version Detection:
#    - Reads the plugin version from version/version.go
#
# 2. Build:
#    - Rebuilds the plugin binary so syft's binary cataloger reflects the
#      current dependency versions from go.mod/go.sum
#
# 3. SBOM Generation:
#    - Creates SBOM using syft in CycloneDX XML format
#    - Scans the Go module source directory for dependencies
#    - Generates a comprehensive dependency inventory from go.sum
#    - Output: sbom.cdx.xml
#
# 4. SBOM Formatting:
#    - Makes the SBOM XML more human readable using xmllint
#
# 5. SBOM Validation:
#    - Validates the generated SBOM using the cyclonedx validator
#    - Ensures SBOM conforms to CycloneDX v1.6 specification
#
# Usage:
#   ./bin/create_sbom.sh
#
# Dependencies:
#   - syft (for SBOM generation)
#   - xmllint (for XML formatting)
#   - cyclonedx (for SBOM validation)
#   - go (for building the plugin binary)
#
# Exit Codes:
#   0  - SBOM created, formatted, and validated successfully
#   1  - Failed to determine plugin version from version/version.go
#   2  - Failed to build the plugin binary
#   3  - Failed to create SBOM using syft
#   4  - Failed to format SBOM XML using xmllint
#   5  - Failed to validate SBOM using cyclonedx
#
# ===============================================================================

script_name="$(basename "${0}")"

printf "%b %b DEBUG: syft version:      <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(syft --version 2>&1 | head -1 | cut -d ' ' -f 2)"
printf "%b %b DEBUG: cyclonedx version: <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(cyclonedx --version 2>&1 | head -1)"

step_text="Determine plugin version from version/version.go"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
plugin_version=$(sed -n 's/^[[:space:]]*Version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' version/version.go | head -1)
if [ -z "${plugin_version}" ]; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 1
fi
printf "%b %b DEBUG: plugin_version=<%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${plugin_version}"
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Build plugin binary"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! go build -o packer-plugin-sylve .; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 2
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Create SBOM using syft"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! syft scan --quiet --source-name packer-plugin-sylve --source-version "${plugin_version}" --output cyclonedx-xml@1.6 dir:. >sbom.cdx.xml; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 3
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Make the sbom.cdx.xml more human readable using xmllint"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! (
    xmllint --format sbom.cdx.xml >sbom.cdx.xml.tmp &&
        mv sbom.cdx.xml.tmp sbom.cdx.xml
); then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 4
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Validate SBOM against CycloneDX v1.6 specification"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! cyclonedx validate --input-version v1_6 --input-file sbom.cdx.xml; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 5
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

exit 0
