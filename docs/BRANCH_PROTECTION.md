<!-- SPDX-License-Identifier: BSD-2-Clause -->
<!-- Copyright (c) 2026, Timo Pallach (timo@pallach.de). -->

# Branch protection (GitHub)

Optional hardening after the repository is public and CI is stable. Configure in the GitHub UI:
`Settings` → `Branches` → `Add branch protection rule` (or edit an existing rule).

## Suggested settings for `main`

- **Require a pull request before merging** — forces review and keeps direct pushes off `main` (aligns with
  `.cursor/rules/development-workflow.mdc`).
- **Require status checks to pass** — enable the checks from `.github/workflows/ci.yml` (e.g. the `test` job)
  so `go test` must pass before merge.
- **Require branches to be up to date before merging** — reduces merge skew.
- **Do not allow bypassing the above settings** — restrict who can push or dismiss reviews per team policy.

## `development` (if used)

Apply the same pattern with checks appropriate for that branch, or protect only `main` and use `development`
as a fast-moving integration branch per your workflow.
