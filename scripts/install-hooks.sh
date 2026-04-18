#!/usr/bin/env bash
# Installs the repo's git hooks for this clone. Idempotent.
#
# Usage:
#   bash desktop/scripts/install-hooks.sh
#
# Rationale: checked-in hook scripts + a one-shot installer is the least
# painful cross-platform equivalent of `core.hooksPath`. Devs who forget
# this command still pass CI lint — the hook just shortens their feedback
# loop locally.
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
src="$repo_root/desktop/scripts/pre-commit.sh"
dst="$repo_root/.git/hooks/pre-commit"

if [ ! -f "$src" ]; then
  echo "install-hooks: source hook not found at $src"
  exit 1
fi

mkdir -p "$(dirname "$dst")"
cp "$src" "$dst"
chmod +x "$dst"
echo "install-hooks: pre-commit hook installed at $dst"
