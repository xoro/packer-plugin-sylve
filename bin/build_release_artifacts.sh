#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# Build Manual GitHub Release Artifacts
# ===============================================================================
#
# Cross-compiles packer-plugin-sylve for darwin/arm64 and freebsd/amd64, writes
# zip archives and a SHA256SUMS file under release-artifacts/<version>/ using the
# same naming as .goreleaser.yml (for tags v0.1.0 and v0.1.1 where GoReleaser is
# skipped in CI). See docs/GITHUB_RELEASES.md.
#
# The script performs the following tasks:
# 1. Read version from version/version.go and module path from go.mod
# 2. Build two binaries with production ldflags (Version, empty VersionPrerelease)
# 3. Zip each binary with the Packer plugin filename pattern
# 4. Write packer-plugin-sylve_v<version>_SHA256SUMS
#
# Usage:
#   ./bin/build_release_artifacts.sh
#
# Dependencies:
# - go
# - zip
# - sha256sum or shasum (macOS)
#
# Exit Codes:
# 0 - Success
# 1 - Could not change to repository root
# 2 - Failed to read version or module path
# 3 - Build or zip failed
#
# ===============================================================================

set -eu

script_name="$(basename "${0}")"
repo_root="$(cd "$(dirname "${0}")/.." && pwd)"
cd "${repo_root}" || exit 1

step_text="Reading version and module path from repository"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
plugin_fqn="$(grep -E '^module' go.mod | sed 's/module[[:space:]]*//')"
version="$(grep '^\tVersion' version/version.go | sed 's/.*"\(.*\)"/\1/')"
if [ -z "${plugin_fqn}" ] || [ -z "${version}" ]; then
    printf "%b %b ERROR: ==>> FAILED: empty plugin_fqn or version\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 2
fi
printf "%b %b DEBUG: plugin_fqn=<%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${plugin_fqn}"
printf "%b %b DEBUG: version=<%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${version}"
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Keep in sync with .goreleaser.yml (env API_VERSION) and the Packer plugin SDK.
api_version="x5.0"

outdir="${repo_root}/release-artifacts/${version}"
tmpdir=""
tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

step_text="Building and zipping darwin/arm64 and freebsd/amd64 binaries"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! mkdir -p "${outdir}"; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 3
fi

build_and_zip() {
    _goos="${1}"
    _goarch="${2}"
    _bin_name="packer-plugin-sylve_v${version}_${api_version}_${_goos}_${_goarch}"
    _zip_name="${_bin_name}.zip"
    if ! GOOS="${_goos}" GOARCH="${_goarch}" CGO_ENABLED=0 go build -trimpath \
        -ldflags="-s -w -X '${plugin_fqn}/version.Version=${version}' -X '${plugin_fqn}/version.VersionPrerelease='" \
        -o "${tmpdir}/${_bin_name}" .; then
        return 1
    fi
    if ! (
        cd "${tmpdir}" && zip -q "${outdir}/${_zip_name}" "${_bin_name}"
    ); then
        return 1
    fi
    rm -f "${tmpdir}/${_bin_name}"
    return 0
}

if ! build_and_zip darwin arm64; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 3
fi
if ! build_and_zip freebsd amd64; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 3
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Writing SHA256SUMS"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if command -v sha256sum >/dev/null 2>&1; then
    if ! (
        cd "${outdir}" && sha256sum \
            "packer-plugin-sylve_v${version}_${api_version}_darwin_arm64.zip" \
            "packer-plugin-sylve_v${version}_${api_version}_freebsd_amd64.zip" \
            >"packer-plugin-sylve_v${version}_SHA256SUMS"
    ); then
        printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
        exit 3
    fi
elif command -v shasum >/dev/null 2>&1; then
    if ! (
        cd "${outdir}" && shasum -a 256 \
            "packer-plugin-sylve_v${version}_${api_version}_darwin_arm64.zip" \
            "packer-plugin-sylve_v${version}_${api_version}_freebsd_amd64.zip" \
            >"packer-plugin-sylve_v${version}_SHA256SUMS"
    ); then
        printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
        exit 3
    fi
else
    printf "%b %b ERROR: ==>> FAILED: need sha256sum or shasum in PATH\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 3
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

printf "%b %b INFO:  Output directory: <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${outdir}"
