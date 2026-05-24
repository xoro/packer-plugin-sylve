---
applyTo: "**/.github/instructions/**"
---

<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# Instruction File Standards

## Overview

This document defines the standards for creating and maintaining GitHub Copilot instruction
files (`.instructions.md`) in this project. All instruction files live in
`.github/instructions/` and are automatically loaded by GitHub Copilot when the `applyTo`
pattern matches the file being edited.

## File Format

### Naming Convention

```
.github/instructions/<topic>.instructions.md
```

- Use lowercase kebab-case for the topic name
- Always use the `.instructions.md` extension
- Name should clearly describe the scope (e.g., `shell-scripts`, `git-commit`, `terminal`)

### Required Structure

Every instruction file must have:

1. **YAML frontmatter** with `applyTo` pattern
2. **BSD-2-Clause license header** (HTML comment format)
3. **Markdown content** with clear, actionable rules

```markdown
---
applyTo: "<glob-pattern>"
---

<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# Title

## Overview

Brief description of what this instruction covers and why it exists.

## Rules

Actionable rules with examples.
```

### Frontmatter: `applyTo` Patterns

The `applyTo` field determines which files trigger this instruction to load. Use
comma-separated glob patterns:

| Pattern                                               | Matches                                   |
| ----------------------------------------------------- | ----------------------------------------- |
| `"**"`                                                | All files (use for universal rules)       |
| `"**/*.sh,**/Makefile,**/bin/*"`                      | Shell scripts, Makefiles, and bin scripts |
| `"**/*.sh,**/*.md,**/Makefile"`                       | Shell scripts, Markdown, and Makefiles    |
| `"**/bin/*.sh,**/bin/*.ps1"`                          | Scripts in bin/ directories               |
| `"**/vulnerabilities/**,**/penetration_test_report*"` | Security-related files                    |

**Guidelines:**

- Use `"**"` for rules that apply universally (coding standards, commit messages, workflows)
- Use specific patterns for file-type-specific rules (shell standards, test scripts)
- Separate multiple patterns with commas — no spaces after the comma
- Quote the entire value in the YAML frontmatter

## Content Standards

### Writing Style

- Use imperative mood ("Use long-form options" not "You should use long-form options")
- Be specific and actionable — provide examples for every rule
- Include both correct and incorrect examples where helpful
- Explain the rationale briefly (why the rule exists)

### Organisation

- One concern per instruction file
- Keep files focused — split large topics into separate files
- Cross-reference related instruction files by name (e.g., "See `shell-scripts.instructions.md`")

### What NOT to Include

- Do not duplicate content across instruction files
- Do not include project-specific commands (those belong in `AGENTS.md`)
- Do not add placeholder or empty sections
- Do not include content that changes frequently (use `AGENTS.md` for that)

## Updating Instructions

When adding a new instruction file:

1. Create the file in `.github/instructions/`
2. Add a reference to `.github/copilot-instructions.md`
3. Add a row to the rule files table in `AGENTS.md`
4. Run `./bin/format_code.sh` and `./bin/run_linter_checks.sh`
