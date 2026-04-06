#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2018 - 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# FlyingDdns Git Hooks Installation Script
# ===============================================================================
#
# This script installs Git hooks to ensure code quality and consistency
# before commits. It sets up pre-commit hooks that run the project's
# quality assurance scripts automatically.
#
# The script performs the following tasks:
# 1. Verifies we're in a Git repository
# 2. Creates the .git/hooks directory if it doesn't exist
# 3. Installs a pre-commit hook that runs format and linter checks and secret scanning
# 4. Makes the pre-commit hook executable
# 5. Installs a commit-msg hook that validates conventional commit format
# 6. Makes the commit-msg hook executable
# 7. Verifies hook installation
#
# Usage:
#   ./bin/install_git_hooks.sh
#
# Dependencies:
#   - Git repository
#   - test/bin/run_license_check.sh
#   - test/bin/run_format_checks.sh
#   - test/bin/run_pipeline_checks.sh
#   - gitleaks (for security scanning)
#
# Exit Codes:
#   0  - Git hooks installed successfully
#   1  - Not in a Git repository, failed to create hooks directory, failed to create pre-commit hook, failed to create commit-msg hook, or hook installation verification failed
#   2  - Failed to create pre-commit hook or commit-msg hook
#   3  - Failed to make pre-commit hook or commit-msg hook executable
#
# ===============================================================================

script_name="$(basename "${0}")"

# Function to print status output
print_status() {
    message="$1"
    printf "%s %s %s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${script_name}" "${message}"
}

print_status "Starting Git hooks installation..."

# Check if we're in a Git repository
if ! git rev-parse --git-dir >/dev/null 2>&1; then
    print_status "ERROR: Not in a Git repository. Please run this script from a Git repository root."
    exit 1
fi

# Get the root directory of the git repository
root_dir="$(git rev-parse --show-toplevel)"
print_status "Repository root: ${root_dir}"

# Create hooks directory if it doesn't exist
print_status "Creating .git/hooks directory..."
if ! mkdir -p "${root_dir}/.git/hooks"; then
    print_status "ERROR: Failed to create .git/hooks directory"
    exit 1
fi

# Create pre-commit hook
print_status "Installing pre-commit hook..."
cat >"${root_dir}/.git/hooks/pre-commit" <<'EOF'
#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2018 - 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# FlyingDdns Pre-commit Hook
# ===============================================================================
#
# This hook runs before each commit to ensure code quality and consistency.
# It performs the following checks:
# 1. License compliance verification
# 2. Code formatting verification
# 3. Security scanning (gitleaks)
# 4. GitLab CI pipeline configuration validation
#
# If any check fails, the commit is prevented and the user is notified.
#
# ===============================================================================

hook_name="pre-commit"

# Function to print status output
print_status() {
    message="$1"
    printf "%s %s %s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${hook_name}" "${message}"
}

# Get the root directory of the git repository
root_dir="$(git rev-parse --show-toplevel)"

print_status "Running pre-commit quality checks..."

# Step 1: Run format checks
print_status "Step 1/3: Checking code formatting..."
if ! "${root_dir}/bin/run_format_checks.sh"; then
    print_status "ERROR: Code formatting check failed"
    print_status "Please run './bin/format_code.sh' to fix formatting issues"
    exit 1
fi
print_status "SUCCESS: Code formatting check passed"

# Step 2: Run linter checks
print_status "Step 2/3: Running linter checks..."
if ! "${root_dir}/bin/run_linter_checks.sh"; then
    print_status "ERROR: Linter checks failed"
    print_status "Please fix the issues reported above before committing"
    exit 1
fi
print_status "SUCCESS: Linter checks passed"

# Step 3: Run secret scanning
print_status "Step 3/3: Running secret scan (gitleaks)..."
if ! "${root_dir}/bin/run_leak_checks.sh"; then
    print_status "ERROR: Secret scan failed - potential secrets detected"
    print_status "Please review and remove any secrets before committing"
    exit 1
fi
print_status "SUCCESS: Secret scan passed"

print_status "All pre-commit checks passed successfully!"
exit 0
EOF

# Check if the pre-commit hook was created successfully
if [ ! -f "${root_dir}/.git/hooks/pre-commit" ]; then
    print_status "ERROR: Failed to create pre-commit hook"
    exit 2
fi

# Make the hook executable
print_status "Making pre-commit hook executable..."
if ! chmod +x "${root_dir}/.git/hooks/pre-commit"; then
    print_status "ERROR: Failed to make pre-commit hook executable"
    exit 3
fi

# Create commit-msg hook for conventional commits
print_status "Installing commit-msg hook for conventional commits..."
cat >"${root_dir}/.git/hooks/commit-msg" <<'EOF'
#!/bin/sh

# SPDX-License-Identifier: BSD-2-Clause
# Copyright (c) 2018 - 2026, Timo Pallach (timo@pallach.de).

# ===============================================================================
# FlyingDdns Commit Message Hook
# ===============================================================================
#
# This hook validates commit messages to ensure they follow conventional
# commit format. It checks for:
# 1. Proper conventional commit structure
# 2. Valid commit types
# 3. Appropriate message length
#
# ===============================================================================

hook_name="commit-msg"

# Function to print status output
print_status() {
    message="$1"
    printf "%s %s %s\n" "$(date "+%Y-%m-%d %H:%M:%S")" "${hook_name}" "${message}"
}

# Get the commit message file
commit_msg_file="$1"

# Read the commit message
commit_msg=$(cat "$commit_msg_file")

# Check if it's a conventional commit
if ! echo "$commit_msg" | grep -qE '^(feat|fix|docs|style|refactor|perf|test|build|ci|chore)(\([a-z-]+\))?: .+'; then
    print_status "ERROR: Commit message does not follow conventional commit format"
    print_status "Expected format: <type>[optional scope]: <description>"
    print_status "Valid types: feat, fix, docs, style, refactor, perf, test, build, ci, chore"
    print_status "Example: feat(auth): add user authentication"
    exit 1
fi

# Check subject line length (120 characters)
subject_line=$(echo "$commit_msg" | head -n1)
if [ ${#subject_line} -gt 120 ]; then
    print_status "ERROR: Commit message subject line is too long (${#subject_line} > 120 characters)"
    print_status "Please shorten the subject line"
    exit 1
fi

print_status "SUCCESS: Commit message validation passed"
exit 0
EOF

# Check if the commit-msg hook was created successfully
if [ ! -f "${root_dir}/.git/hooks/commit-msg" ]; then
    print_status "ERROR: Failed to create commit-msg hook"
    exit 2
fi

# Make the commit-msg hook executable
if ! chmod +x "${root_dir}/.git/hooks/commit-msg"; then
    print_status "ERROR: Failed to make commit-msg hook executable"
    exit 3
fi

# Verify installation
print_status "Verifying hook installation..."
if [ -x "${root_dir}/.git/hooks/pre-commit" ] && [ -x "${root_dir}/.git/hooks/commit-msg" ]; then
    print_status "SUCCESS: Git hooks installed successfully!"
    print_status "SUCCESS: Pre-commit hook: Will run format checks, linter checks, and secret scanning"
    print_status "SUCCESS: Commit-msg hook: Will validate conventional commit format"
    print_status "Hooks will now run automatically on every commit"
else
    print_status "ERROR: Hook installation verification failed"
    exit 1
fi

print_status "Git hooks installation completed successfully!"
exit 0
