<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# GitHub Copilot Instructions for packer-plugin-sylve

## Overview

All coding standards, best practices, and development guidelines for this project are defined in the `.cursor/rules/` directory. **Follow all rules defined in those files.**

## Rule Files Reference

The following rules apply to this project. Refer to each file for detailed standards:

### Universal Standards

- **`.cursor/rules/general-coding-standards.mdc`** - Universal coding standards for ALL file types
  - CRITICAL: NO emojis or visual indicators in source code, config, or data files
  - Plain ASCII text only (emojis allowed ONLY in .md/.mdc documentation files)

### Language and File-Type Rules

- **`.cursor/rules/shell-scripts.mdc`** - Shell script development standards (POSIX, step pattern, log format)
- **`.cursor/rules/license-header.mdc`** - BSD-2-Clause license header requirements for all source files

### Development Workflow

- **`.cursor/rules/development-workflow.mdc`** - Git branch strategy and release process
- **`.cursor/rules/git-commit.mdc`** - Conventional commit message format and workflow
- **`.cursor/rules/test-scripts.mdc`** - Quality script inventory, execution standards, and retry workflow

### Code Standards

- **`.cursor/rules/command-line-options.mdc`** - Use long-form CLI options
- **`.cursor/rules/command-output.mdc`** - Show complete unfiltered command output
- **`.cursor/rules/terminal.mdc`** - Terminal usage standards

### Agent Behaviour

- **`.cursor/rules/coding-agent-guidelines.mdc`** - Mandatory guidelines for coding agents creating or modifying code
- **`.cursor/rules/cursor-rule-standards.mdc`** - Standards for rule files themselves

### Security

- **`.cursor/rules/vulnerability-remediation.mdc`** - Penetration test vulnerability remediation workflow

## Key Principles

1. **No Duplication**: These rules are the single source of truth
2. **No Emojis**: Never use emojis in code, config, or data files (only in .md/.mdc docs)
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

When working on specific file types or tasks, **read the corresponding rule file** from `.cursor/rules/` for complete, detailed standards.

## AGENTS.md

For comprehensive project documentation, commands, and development workflow, see **`AGENTS.md`** in the project root.
