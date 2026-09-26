#!/usr/bin/env bash
# Fail when the current HEAD is behind origin/main (a stale base).
#
# Paseo has repeatedly handed worktrees a stale local `main` (98-150 commits
# behind); lanes then implement against code that no longer exists. This guard
# catches that before any work is spent.
#
# Offline is NOT fatal: if the fetch cannot reach origin we warn and skip
# rather than hard-fail, so a network partition never blocks a lane. The CI
# gate (ci.yml) still enforces this on merge.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

base="origin/main"

# Refresh the remote tracking ref. Tolerate no-network gracefully.
if ! git fetch --quiet origin main; then
  if git rev-parse --verify --quiet "$base" >/dev/null 2>&1; then
    echo "base-check: WARNING could not reach origin; using cached $base" >&2
  else
    echo "base-check: WARNING could not reach origin and no cached $base; skipping" >&2
    exit 0
  fi
fi

if ! git rev-parse --verify --quiet "$base" >/dev/null 2>&1; then
  echo "base-check: WARNING $base unavailable; skipping" >&2
  exit 0
fi

head_sha="$(git rev-parse --short HEAD)"
base_sha="$(git rev-parse --short "$base")"

# Commits reachable from origin/main but not HEAD => HEAD is behind (stale).
behind="$(git rev-list --count HEAD.."$base")"

echo "base-check: HEAD=$head_sha origin/main=$base_sha"

if [ "$behind" -gt 0 ]; then
  echo "base-check: FAIL HEAD is $behind commit(s) behind $base ($base_sha)." >&2
  echo "base-check:       the tree was based on a stale main; rebase first:" >&2
  echo "base-check:         git fetch origin && git merge --ff-only origin/main" >&2
  exit 1
fi

echo "base-check: OK (HEAD is not behind $base)"
