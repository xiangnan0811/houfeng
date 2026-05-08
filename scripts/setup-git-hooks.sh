#!/bin/sh
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "setup-git-hooks: run this script inside the houfeng git repository" >&2
  exit 1
}

cd "$repo_root"

for hook in pre-commit pre-merge-commit pre-push pre-rebase; do
  if [ ! -f ".githooks/$hook" ]; then
    echo "setup-git-hooks: missing .githooks/$hook" >&2
    exit 1
  fi
done

git config core.hooksPath .githooks
chmod +x .githooks/pre-commit .githooks/pre-merge-commit .githooks/pre-push .githooks/pre-rebase

echo "Configured git core.hooksPath=.githooks"
