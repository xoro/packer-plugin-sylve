<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# GitHub Copilot Instructions for packer-plugin-sylve

## Overview

All coding standards, best practices, and development guidelines for this project are defined in the `.github/instructions/` directory. **Follow all rules defined in those files.**

## Rule Files Reference

The following rules apply to this project. Refer to each file for detailed standards:

### Universal Standards

- **`.github/instructions/general-coding-standards.instructions.md`** - Universal coding standards for ALL file types
  - CRITICAL: NO emojis or visual indicators in source code, config, or data files
  - Plain ASCII text only (emojis allowed ONLY in .md documentation files)

### Language and File-Type Rules

- **`.github/instructions/shell-scripts.instructions.md`** - Shell script development standards (POSIX, step pattern, log format)
- **`.github/instructions/license-header.instructions.md`** - BSD-2-Clause license header requirements for all source files

### Development Workflow

- **`.github/instructions/development-workflow.instructions.md`** - Git branch strategy and release process
- **`.github/instructions/git-commit.instructions.md`** - Conventional commit message format and workflow
- **`.github/instructions/test-scripts.instructions.md`** - Quality script inventory, execution standards, and retry workflow

### Code Standards

- **`.github/instructions/command-line-options.instructions.md`** - Use long-form CLI options
- **`.github/instructions/command-output.instructions.md`** - Show complete unfiltered command output
- **`.github/instructions/terminal.instructions.md`** - Terminal usage standards

### Agent Behaviour

- **`.github/instructions/coding-agent-guidelines.instructions.md`** - Mandatory guidelines for coding agents creating or modifying code
- **`.github/instructions/instruction-file-standards.instructions.md`** - Standards for instruction files themselves

### Security

- **`.github/instructions/vulnerability-remediation.instructions.md`** - Penetration test vulnerability remediation workflow

## Key Principles

1. **No Duplication**: These rules are the single source of truth
2. **No Emojis**: Never use emojis in code, config, or data files (only in .md docs)
3. **Conventional Commits**: All commits must follow the conventional commit format
4. **Long-Form Options**: Use `--verbose` not `-v` in shell scripts and documentation
5. **BSD-2-Clause License**: All source files must have license headers
6. **Development Branch**: All work happens on `development`, never commit directly to `main`
7. **POSIX Shell**: All shell scripts use `#!/bin/sh`, never `#!/bin/bash`

## Quality Checks

Before every commit, run:

```bash
./bin/format_code.sh          # Apply formatting
./bin/run_format_checks.sh    # Verify formatting is correct
./bin/run_linter_checks.sh    # Run all linters
go test ./...                 # Run unit tests
```

## Important: Read the Rule Files

When working on specific file types or tasks, **read the corresponding rule file** from `.github/instructions/` for complete, detailed standards.

## AGENTS.md

For comprehensive project documentation, commands, and development workflow, see **`AGENTS.md`** in the project root.
