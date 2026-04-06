#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# packer-plugin-sylve Go Module Update Script
# ===============================================================================
#
# This script updates all Go module dependencies to their latest available
# versions, tidies the module graph, and verifies that the project still
# builds and passes linting after the update.
#
# The script performs the following tasks:
# 1. Dependency Update:
#    - Runs go get -u ./... to upgrade all direct and indirect dependencies
#      to the latest minor/patch releases
#
# 2. Module Graph Tidy:
#    - Runs go mod tidy to remove unused dependencies and add any missing ones
#
# 3. Module Verification:
#    - Runs go mod verify to confirm all downloaded modules match go.sum
#
# 4. Build Verification:
#    - Runs go build ./... to confirm the project compiles after the update
#
# Usage:
#   ./bin/update_go_mods.sh
#
# Dependencies:
#   - go (Go toolchain)
#
# Exit Codes:
#   0  - All steps completed successfully
#   1  - Failed to update Go module dependencies
#   2  - Failed to tidy Go module graph
#   3  - Failed to verify Go modules
#   4  - Failed to build the project after dependency update
#
# ===============================================================================

script_name="$(basename "${0}")"

printf "%b %b DEBUG: go version: <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(go version)"

step_text="Update Go module dependencies"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! go get -u ./...; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 1
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Tidy Go module graph"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! go mod tidy; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 2
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Verify Go modules"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! go mod verify; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 3
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Build project after dependency update"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! go build ./...; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 4
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

exit 0
