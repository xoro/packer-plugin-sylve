#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# packer-plugin-sylve Code Formatting Script
# ===============================================================================
#
# This script formats all code files in the packer-plugin-sylve codebase to
# ensure consistent formatting across different file types. It applies
# formatting rules to maintain code style standards throughout the project.
#
# The script performs the following tasks:
# 1. Markdown Table of Contents Generation:
#    - Generates/updates TOC in README.md with markdown-toc
#    - Ensures documentation structure is maintained
#
# 2. Unified Code Formatting via treefmt:
#    - Formats all file types declared in treefmt.toml in a single pass
#    - Runs formatters in parallel with file-level caching for fast incremental runs
#    - Covered formatters: gofmt (Go), shfmt (Shell), packer fmt (HCL),
#      taplo (TOML), clang-format (C), prettier (Markdown, JSON)
#
# This script is typically run during development to maintain consistent
# code formatting across the entire codebase.
#
# Usage:
#   ./bin/format_code.sh
#
# Dependencies:
#   - markdown-toc (for Markdown table of contents)
#   - treefmt (unified formatter, see treefmt.toml for per-formatter config)
#
# Exit Codes:
#   0  - All formatting completed successfully
#   1  - Failed to generate markdown table of contents
#   2  - treefmt formatting failed
#
# ===============================================================================

script_name="$(basename "${0}")"

printf "%b %b DEBUG: treefmt version:       <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(treefmt --version | cut -d " " -f 2)"
printf "%b %b DEBUG: markdown-toc version:  <%b>\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "$(node -e "console.log(require('$(dirname "$(readlink -f "$(command -v markdown-toc)")")/package.json').version)")"

# Markdown table of contents generator
step_text="Generate markdown table of contents"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
find . -not -path './.git/*' -name '*.md' | while IFS= read -r md_file; do
    if ! markdown-toc -i "${md_file}"; then
        printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
        exit 1
    fi
done
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

# Unified formatting via treefmt
step_text="Run treefmt for unified formatting"
printf "\n%b %b INFO:  ==>> STEP: %b:\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
if ! treefmt; then
    printf "%b %b ERROR: ==>> FAILED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"
    exit 2
fi
printf "%b %b INFO:  ==>> SUCCEEDED: %b\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${step_text}"

exit 0
