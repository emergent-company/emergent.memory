#!/usr/bin/env bash
# Fail when the current HEAD is behind origin/main (a stale base).
#
# Paseo has repeatedly handed worktrees a stale local `main` (98-150 commits
# behind); lanes then implement against code that no longer exists. This guard
# catches that before any work is spent.
#
# Offline is NOT fatal: if the fetch cannot reach origin we warn and skip
# rather than hard-fail, so a network partition never blocks a lane.
#
# This is a LOCAL pre-push aid. In CI, actions/checkout checks out a detached
# synthetic merge commit (refs/pull/N/merge) that is not a descendant of
# origin/main, so `HEAD..origin/main` is meaningless there and reports a bogus
# count. Branch protection's "require branches up to date before merging"
# already enforces freshness on merge, so we skip (exit 0) in CI instead.
set -euo pipefail

# --warn: advisory mode — report staleness but exit 0. Used by the pre-push
# hook, where hard-failing is redundant with branch protection's "up to date
# before merge" and only trains `git push --no-verify` (disabling every
# pre-push guard). The default (no flag) hard-fails, which is kept for manual
# runs and scripts/preflight/all.sh.
warn=0
if [ "${1:-}" = "--warn" ]; then
  warn=1
fi

root="$(git rev-parse --show-toplevel)"
cd "$root"

# CI context: the synthetic merge commit is not a linear descendant of main,
# so the stale-base comparison below would false-fail. Skip and let branch
# protection enforce freshness.
if [ -n "${CI:-}" ] || [ -n "${GITHUB_ACTIONS:-}" ]; then
  echo "base-check: skipped in CI (branch protection enforces up-to-date before merge)"
  exit 0
fi

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
  if [ "$warn" -eq 1 ]; then
    echo "base-check: WARN HEAD is $behind commit(s) behind $base ($base_sha)." >&2
    echo "base-check:       rebase before opening/merging a PR:" >&2
    echo "base-check:         git fetch origin && git merge --ff-only origin/main" >&2
    exit 0
  fi
  echo "base-check: FAIL HEAD is $behind commit(s) behind $base ($base_sha)." >&2
  echo "base-check:       the tree was based on a stale main; rebase first:" >&2
  echo "base-check:         git fetch origin && git merge --ff-only origin/main" >&2
  exit 1
fi

echo "base-check: OK (HEAD is not behind $base)"
