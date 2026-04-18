#!/usr/bin/env bash
# Pre-commit hook for taskflow-desktop. Mirrors the CI lint job (see
# .github/workflows/lint.yml) so bugs that would fail CI are caught on
# the laptop first.
#
# Install:
#   bash desktop/scripts/install-hooks.sh
#
# The hook is intentionally non-skipping — if you need to bypass it for a
# WIP commit, use `git commit --no-verify`. Do NOT add --no-verify to any
# scripted/automated commit flow.
set -euo pipefail

cd "$(dirname "$0")/.."

# 1. Formatting
unformatted="$(gofmt -l . 2>/dev/null || true)"
if [ -n "$unformatted" ]; then
  echo "pre-commit: gofmt violations:"
  echo "$unformatted"
  echo "Run: gofmt -w ."
  exit 1
fi

# 2. Vet — catches many of the sync.Mutex misuses, unreachable code, and
#    context leaks flagged in the audit.
go vet ./...

# 3. Staticcheck — optional (only runs if installed). Install with:
#      go install honnef.co/go/tools/cmd/staticcheck@latest
if command -v staticcheck >/dev/null 2>&1; then
  staticcheck ./...
else
  echo "pre-commit: staticcheck not installed — skipping (install with 'go install honnef.co/go/tools/cmd/staticcheck@latest')"
fi

# 4. Errcheck — catches ignored error returns (would have caught H-AUTH-3).
if command -v errcheck >/dev/null 2>&1; then
  errcheck ./...
else
  echo "pre-commit: errcheck not installed — skipping (install with 'go install github.com/kisielk/errcheck@latest')"
fi

echo "pre-commit OK"
