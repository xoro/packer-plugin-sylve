#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# Publish Manual GitHub Release Artifacts
# ===============================================================================
#
# Builds release zips (optional) and uploads them to a GitHub Release for the
# matching tag (v0.1.0 / v0.1.1 style when GoReleaser is not used). Requires the
# GitHub CLI (gh) and a logged-in account with release write access.
#
# The script performs the following tasks:
# 1. Take semver from required --version (e.g. 0.1.0)
# 2. Optionally run bin/build_release_artifacts.sh
# 3. Verify zip and SHA256SUMS files exist under release-artifacts/<version>/
# 4. Upload with gh release upload, or create the release with gh release create
#
# Usage:
#   ./bin/publish_github_release_artifacts.sh --version 0.1.0
#   ./bin/publish_github_release_artifacts.sh --version 0.1.0 --no-build
#   ./bin/publish_github_release_artifacts.sh --version 0.1.0 --create
#
#   --no-build   Skip building; only upload existing files in release-artifacts/
#   --create     Use "gh release create" (first publish). Fails if the release exists.
#                Without --create, uses "gh release upload" (replace assets with --clobber).
#
# Dependencies:
# - gh (https://cli.github.com/)
# - Same as build_release_artifacts.sh when not using --no-build
#
# Exit Codes:
# 0 - Success
# 1 - Could not change to repository root
# 2 - Bad arguments or missing artifact files
# 3 - Build helper failed
# 4 - gh command failed
#
# ===============================================================================

set -eu

script_name="$(basename "${0}")"
repo_root="$(cd "$(dirname "${0}")/.." && pwd)"
cd "${repo_root}" || exit 1

version_override=""
run_build=1
create_mode=0

while [ "$#" -gt 0 ]; do
    case "$1" in
    --version)
        if [ "$#" -lt 2 ]; then
            printf "%b %b ERROR: --version requires a value (e.g. 0.1.0)\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
            exit 2
        fi
        version_override="$2"
        shift 2
        ;;
    --no-build)
        run_build=0
        shift
        ;;
    --create)
        create_mode=1
        shift
        ;;
    --help | -h)
        printf "Usage: %s --version <X.Y.Z> [--no-build] [--create]\n" "${script_name}"
        printf "  Build (unless --no-build) and publish release-artifacts to GitHub.\n"
        printf "  Default: gh release upload (existing release). --create: gh release create.\n"
        exit 0
        ;;
    *)
        printf "%b %b ERROR: unknown argument: <%b> (try --help)\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$1"
        exit 2
        ;;
    esac
done

if [ -z "${version_override}" ]; then
    printf "%b %b ERROR: --version is required (e.g. 0.1.0)\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 2
fi

if ! printf '%s\n' "${version_override}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    printf "%b %b ERROR: --version must be MAJOR.MINOR.PATCH (digits and dots only), got <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${version_override}"
    exit 2
fi

version="${version_override}"
tag="v${version}"

# Keep in sync with .goreleaser.yml and build_release_artifacts.sh.
api_version="x5.0"

artifact_dir="${repo_root}/release-artifacts/${version}"
zip_darwin="${artifact_dir}/packer-plugin-sylve_v${version}_${api_version}_darwin_arm64.zip"
zip_freebsd="${artifact_dir}/packer-plugin-sylve_v${version}_${api_version}_freebsd_amd64.zip"
sums="${artifact_dir}/packer-plugin-sylve_v${version}_SHA256SUMS"
changelog="${repo_root}/CHANGELOG.md"

if [ "${run_build}" -eq 1 ]; then
    step_text="Building release artifacts via bin/build_release_artifacts.sh"
    printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    if ! "${repo_root}/bin/build_release_artifacts.sh" --version "${version}"; then
        printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
        exit 3
    fi
    printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
fi

step_text="Verifying artifact files exist"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
for f in "${zip_darwin}" "${zip_freebsd}" "${sums}"; do
    if [ ! -f "${f}" ]; then
        printf "%b %b ERROR: missing file <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${f}"
        printf "%b %b ERROR: run without --no-build or ./bin/build_release_artifacts.sh --version %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${version}"
        exit 2
    fi
done
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

if ! command -v gh >/dev/null 2>&1; then
    printf "%b %b ERROR: gh is not in PATH; install GitHub CLI: https://cli.github.com/\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 4
fi

if [ "${create_mode}" -eq 1 ]; then
    if [ ! -f "${changelog}" ]; then
        printf "%b %b ERROR: --create requires <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${changelog}"
        exit 2
    fi
    step_text="Creating GitHub release and uploading assets (gh release create)"
    printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    if ! gh release create "${tag}" \
        --title "${tag}" \
        --notes-file "${changelog}" \
        "${zip_darwin}" \
        "${zip_freebsd}" \
        "${sums}"; then
        printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
        exit 4
    fi
else
    step_text="Uploading assets to GitHub release (gh release upload --clobber)"
    printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    if ! gh release upload "${tag}" \
        "${zip_darwin}" \
        "${zip_freebsd}" \
        "${sums}" \
        --clobber; then
        printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
        printf "%b %b ERROR: If the release does not exist yet, run with --create (requires CHANGELOG.md).\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
        exit 4
    fi
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

printf "%b %b INFO:  Published assets for release <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${tag}"
