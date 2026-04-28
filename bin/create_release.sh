#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# Create Patch Release (bump, changelog, commit, tag, optional push)
# ===============================================================================
#
# End-to-end release pipeline (this script does the local steps; GitHub does the
# rest after you push the tag):
#
#   create_release.sh --push
#       -> git push origin <branch> && git push origin vX.Y.Z
#       -> tag appears on GitHub
#       -> GitHub Actions workflow ".github/workflows/release.yml" runs on tag v*
#       -> GoReleaser (goreleaser release --clean) builds and uploads artifacts
#       -> GitHub Release is created/updated for the tag with the binaries
#
# Tags v0.1.0 and v0.1.1 are excluded from that workflow (manual releases).
#
# This script:
# - Increments the PATCH in version/version.go (e.g. 0.1.2 -> 0.1.3)
# - Inserts a new section into CHANGELOG.md (git-cliff when available)
# - Commits with chore(release): X.Y.Z and creates annotated tag vX.Y.Z
# - With --push, publishes the branch and tag so the pipeline above runs
#
# The script performs the following tasks:
# 1. Validate clean working tree and expected branch (default: main)
# 2. Compute new semver (patch + 1)
# 3. Generate changelog snippet and insert it after the # Changelog heading
# 4. Update version/version.go
# 5. Run format, tests, and linters (unless --skip-checks)
# 6. Commit, tag, and optionally push (push triggers CI + GoReleaser + GitHub Release)
#
# Usage:
#   ./bin/create_release.sh [--dry-run] [--no-push] [--skip-checks] [--branch <name>]
#
#   --dry-run      Print actions only; no file or git changes
#   --no-push      Skip git push (commit and tag locally only; no GitHub Release)
#   --skip-checks  Skip ./bin/format_code.sh, go test, ./bin/run_linter_checks.sh
#   --branch NAME  Require current branch to be NAME (default: main)
#
# Typical release (from repo root, on main, clean tree):
#   ./bin/create_release.sh
#
# Exit Codes:
# 0 - Success (or dry-run completed)
# 1 - Could not change to repository root
# 2 - Validation failed (dirty tree, wrong branch, version parse)
# 3 - Quality checks failed
# 4 - git commit, tag, or push failed
#
# ===============================================================================

set -eu

script_name="$(basename "${0}")"
repo_root="$(cd "$(dirname "${0}")/.." && pwd)"
cd "${repo_root}" || exit 1

dry_run=0
do_push=1
skip_checks=0
required_branch="main"

while [ "$#" -gt 0 ]; do
    case "$1" in
    --dry-run)
        dry_run=1
        shift
        ;;
    --no-push)
        do_push=0
        shift
        ;;
    --skip-checks)
        skip_checks=1
        shift
        ;;
    --branch)
        if [ "$#" -lt 2 ]; then
            printf "%b %b ERROR: --branch requires a value\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
            exit 2
        fi
        required_branch="$2"
        shift 2
        ;;
    --help | -h)
        printf "Usage: %s [--dry-run] [--no-push] [--skip-checks] [--branch <name>]\n" "${script_name}"
        printf "  Bump PATCH, changelog, chore(release) commit, tag vX.Y.Z, and push (default).\n"
        printf "  Push triggers GitHub Actions -> GoReleaser -> GitHub Release (see %s header).\n" "${script_name}"
        printf "  Use --no-push to commit and tag locally without pushing.\n"
        printf "  Tags v0.1.0 and v0.1.1 skip GoReleaser per .github/workflows/release.yml.\n"
        exit 0
        ;;
    *)
        printf "%b %b ERROR: unknown argument: <%b> (try --help)\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$1"
        exit 2
        ;;
    esac
done

current_branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
if [ -z "${current_branch}" ] || [ "${current_branch}" = "HEAD" ]; then
    printf "%b %b ERROR: detached HEAD; checkout a branch first\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 2
fi

if [ "${current_branch}" != "${required_branch}" ]; then
    printf "%b %b ERROR: expected branch <%b>, on <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${required_branch}" "${current_branch}"
    exit 2
fi

read_version() {
    grep '^\tVersion' "${repo_root}/version/version.go" | sed 's/.*"\(.*\)"/\1/'
}

increment_patch() {
    _ver="$1"
    printf '%s\n' "${_ver}" | awk -F. 'NF == 3 && $1 ~ /^[0-9]+$/ && $2 ~ /^[0-9]+$/ && $3 ~ /^[0-9]+$/ {
        printf "%d.%d.%d\n", $1, $2, $3 + 1
        exit 0
    }
    { exit 1 }'
}

current_version="$(read_version)"
if [ -z "${current_version}" ]; then
    printf "%b %b ERROR: could not read Version from version/version.go\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 2
fi

if ! printf '%s\n' "${current_version}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    printf "%b %b ERROR: expected MAJOR.MINOR.PATCH in version.go, got <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${current_version}"
    exit 2
fi

new_version="$(increment_patch "${current_version}")" || {
    printf "%b %b ERROR: could not increment patch for <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${current_version}"
    exit 2
}

printf "%b %b INFO:  current_version=<%b> new_version=<%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${current_version}" "${new_version}"

if [ "${dry_run}" -eq 1 ]; then
    printf "%b %b INFO:  dry-run: would update version/version.go and CHANGELOG.md, commit, tag v%s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${new_version}"
    if [ "${do_push}" -eq 1 ]; then
        printf "%b %b INFO:  dry-run: would push origin %s and tag v%s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${required_branch}" "${new_version}"
        printf "%b %b INFO:  dry-run: push would trigger GitHub Actions -> GoReleaser -> GitHub Release (see script header).\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    else
        printf "%b %b INFO:  dry-run: --no-push set; would skip git push (local commit and tag only)\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    fi
    exit 0
fi

if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
    printf "%b %b ERROR: working tree is not clean; commit or stash changes first\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 2
fi

changelog_block=""
if command -v git-cliff >/dev/null 2>&1; then
    step_text="Generate changelog snippet with git-cliff"
    printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    if ! changelog_block="$(git-cliff --config "${repo_root}/cliff.toml" --unreleased --tag "v${new_version}" 2>/dev/null)"; then
        printf "%b %b ERROR: ==>> FAILED: git-cliff\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
        exit 2
    fi
    printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
else
    _d="$(date "+%Y-%m-%d")"
    changelog_block="## [${new_version}] - ${_d}

### Release

- Patch release.
"
    printf "%b %b WARN: git-cliff not in PATH; using minimal CHANGELOG section\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
fi

step_text="Insert changelog section and bump version/version.go"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
_changelog_tmp="$(mktemp)"
_version_tmp="$(mktemp)"
trap 'rm -f "${_changelog_tmp}" "${_version_tmp}"' EXIT

# Insert after line 5 (SPDX block + blank + # Changelog + blank) — see CHANGELOG.md layout.
if ! head -n 5 "${repo_root}/CHANGELOG.md" >"${_changelog_tmp}"; then
    printf "%b %b ERROR: ==>> FAILED: reading CHANGELOG.md\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 2
fi
printf '%s\n' "${changelog_block}" >>"${_changelog_tmp}"
if ! tail -n +6 "${repo_root}/CHANGELOG.md" >>"${_changelog_tmp}"; then
    printf "%b %b ERROR: ==>> FAILED: splicing CHANGELOG.md\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 2
fi
if ! mv "${_changelog_tmp}" "${repo_root}/CHANGELOG.md"; then
    exit 2
fi

if ! sed "s/Version = \"[^\"]*\"/Version = \"${new_version}\"/" "${repo_root}/version/version.go" >"${_version_tmp}"; then
    exit 2
fi
if ! mv "${_version_tmp}" "${repo_root}/version/version.go"; then
    exit 2
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
trap - EXIT
rm -f "${_changelog_tmp}" "${_version_tmp}"

if [ "${skip_checks}" -eq 0 ]; then
    step_text="Run format, unit tests, and linters"
    printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    if ! "${repo_root}/bin/format_code.sh"; then
        printf "%b %b ERROR: ==>> FAILED: format_code.sh\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
        exit 3
    fi
    if ! go test ./...; then
        printf "%b %b ERROR: ==>> FAILED: go test\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
        exit 3
    fi
    if ! "${repo_root}/bin/run_linter_checks.sh"; then
        printf "%b %b ERROR: ==>> FAILED: run_linter_checks.sh\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
        exit 3
    fi
    printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
fi

step_text="Commit release (chore(release))"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! git add "${repo_root}/version/version.go" "${repo_root}/CHANGELOG.md"; then
    exit 4
fi
_commit_subject="chore(release): ${new_version}"
_commit_body="Bump Version to ${new_version} and extend CHANGELOG.md for Git tag v${new_version} (GoReleaser)."
if ! git commit --message "${_commit_subject}" --message "" --message "${_commit_body}"; then
    printf "%b %b ERROR: ==>> FAILED: git commit\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 4
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

step_text="Create annotated tag"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! git tag --annotate "v${new_version}" --message "Release v${new_version}"; then
    printf "%b %b ERROR: ==>> FAILED: git tag\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}"
    exit 4
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

if [ "${do_push}" -eq 1 ]; then
    step_text="Push branch and tag to origin"
    printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    if ! git push origin "${required_branch}"; then
        printf "%b %b ERROR: ==>> FAILED: git push origin %s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${required_branch}"
        exit 4
    fi
    if ! git push origin "v${new_version}"; then
        printf "%b %b ERROR: ==>> FAILED: git push origin v%s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${new_version}"
        exit 4
    fi
    printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    printf "%b %b INFO:  Next (automatic on GitHub): Actions workflow release -> GoReleaser -> GitHub Release v%s.\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${new_version}"
    if [ "${new_version}" = "0.1.0" ] || [ "${new_version}" = "0.1.1" ]; then
        printf "%b %b WARN:  Tag v%s is excluded from GoReleaser in release.yml; publish artifacts manually if needed.\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${new_version}"
    fi
else
    printf "%b %b INFO:  Next: git push origin %s && git push origin v%s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${required_branch}" "${new_version}"
    printf "%b %b INFO:  After push, GitHub runs Actions -> GoReleaser -> GitHub Release for v%s.\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${new_version}"
fi

printf "%b %b INFO:  Local release v%s complete (commit + tag v%s).\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${new_version}" "${new_version}"
