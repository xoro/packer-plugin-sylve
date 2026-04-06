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
# 1. Resolve semver (from --version or version/version.go) and read module path from go.mod
# 2. Build two binaries with production ldflags (Version, empty VersionPrerelease)
# 3. Zip each binary with the Packer plugin filename pattern
# 4. Write packer-plugin-sylve_v<version>_SHA256SUMS
#
# Usage:
#   ./bin/build_release_artifacts.sh
#   ./bin/build_release_artifacts.sh --version 0.1.0
#
# With --version, embedded semver and output paths use that value (e.g. rebuild v0.1.0
# artifacts while version/version.go still says a newer release).
#
# Dependencies:
# - go
# - zip
# - sha256sum or shasum (macOS)
#
# Exit Codes:
# 0 - Success
# 1 - Could not change to repository root
# 2 - Bad arguments, invalid --version value, or failed to read version/module path
# 3 - Build or zip failed
#
# ===============================================================================

set -eu

script_name="$(basename "${0}")"
repo_root="$(cd "$(dirname "${0}")/.." && pwd)"
cd "${repo_root}" || exit 1

version_override=""
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
    --help | -h)
        printf "Usage: %s [--version <X.Y.Z>]\n" "${script_name}"
        printf "  Build release zips under release-artifacts/<version>/.\n"
        printf "  If --version is omitted, read semver from version/version.go.\n"
        exit 0
        ;;
    *)
        printf "%b %b ERROR: unknown argument: <%b> (try --help)\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$1"
        exit 2
        ;;
    esac
done

if [ -n "${version_override}" ]; then
    if ! printf '%s\n' "${version_override}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
        printf "%b %b ERROR: --version must be MAJOR.MINOR.PATCH (digits and dots only), got <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${version_override}"
        exit 2
    fi
fi

step_text="Resolving version and reading module path from repository"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
plugin_fqn="$(grep -E '^module' go.mod | sed 's/module[[:space:]]*//')"
if [ -z "${plugin_fqn}" ]; then
    printf "%b %b ERROR: ==>> FAILED: empty plugin_fqn from go.mod\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 2
fi
if [ -n "${version_override}" ]; then
    version="${version_override}"
    printf "%b %b DEBUG: version_source=<%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "--version"
else
    version="$(grep '^\tVersion' version/version.go | sed 's/.*"\(.*\)"/\1/')"
    printf "%b %b DEBUG: version_source=<%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "version/version.go"
fi
if [ -z "${version}" ]; then
    printf "%b %b ERROR: ==>> FAILED: empty version\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
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
