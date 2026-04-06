#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# packer-plugin-sylve Unit Tests Script
# ===============================================================================
#
# This script runs the Go unit test suite for packer-plugin-sylve with coverage
# reporting.
#
# The script performs the following tasks:
# 1. Go Unit Tests:
#    - Runs go test ./... with the race detector enabled
#    - Generates a coverage profile (coverage.out)
#
# 2. Print Coverage Summary:
#    - Prints a per-package coverage summary to stdout
#
# 3. Check Minimum Coverage:
#    - Verifies total statement coverage is at least the value of minimum_coverage
#
# Usage:
#   ./bin/run_unit_tests.sh
#
# Dependencies:
#   - go (Go toolchain, 1.22+)
#
# Exit Codes:
#   0  - All unit tests passed and coverage requirement met
#   1  - One or more unit tests failed
#   2  - Total coverage is below the minimum threshold
#
# ===============================================================================

set -eu

script_name="$(basename "${0}")"
minimum_coverage="98"

step_text="Run Go unit tests with race detector and coverage"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! go test -race -coverprofile=coverage.out -covermode=atomic ./...; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 1
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Print coverage summary"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
coverage_output="$(go tool cover -func=coverage.out)"
printf "%b\n" "${coverage_output}"
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Check minimum coverage threshold (${minimum_coverage}%)"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
total_coverage="$(printf "%b\n" "${coverage_output}" | grep '^total:' | awk '{print $3}' | tr -d '%')"
if ! awk -v total="${total_coverage}" -v min="${minimum_coverage}" 'BEGIN { exit (total < min) }'; then
    printf "%b %b ERROR: Total coverage (%b%%) is below the required %b%%\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${total_coverage}" "${minimum_coverage}"
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 2
fi
printf "%b %b DEBUG: Total coverage (%b%%) meets the required %b%%\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${total_coverage}" "${minimum_coverage}"
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

exit 0
