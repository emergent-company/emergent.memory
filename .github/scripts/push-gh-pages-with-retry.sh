#!/usr/bin/env bash
# Push the current HEAD to `gh-pages` with the same pull-rebase-and-retry loop
# used by .github/scripts/publish-appcast.sh.
#
# Both the docs workflow (mike) and the Mac appcast publisher push to the same
# `gh-pages` branch. They share the `gh-pages-deploy` concurrency group, so
# GitHub serializes their runs; a manual push or a run from another branch can
# still land first, so retry with a rebase to tolerate that residual race. The
# content lives in disjoint directories, so the rebase never conflicts.
#
# Usage: push-gh-pages-with-retry.sh [branch]   (default branch: gh-pages)
# Env:   PUSH_ATTEMPTS (default 5, matching publish-appcast.sh)
set -euo pipefail

branch="${1:-gh-pages}"
attempts="${PUSH_ATTEMPTS:-5}"

for attempt in $(seq 1 "$attempts"); do
  if git push origin "HEAD:${branch}"; then
    echo "pushed ${branch} (attempt ${attempt})"
    exit 0
  fi
  echo "push rejected (attempt ${attempt}); rebasing on latest ${branch}" >&2
  if ! git pull --rebase origin "${branch}" >/dev/null 2>&1; then
    # A rebase conflict is not transient; retrying cannot help. Abort so the
    # job fails cleanly instead of leaving a half-finished rebase behind.
    echo "error: rebase onto origin/${branch} failed; aborting" >&2
    git rebase --abort 2>/dev/null || true
    exit 1
  fi
  sleep 3
done
echo "error: could not push ${branch} after ${attempts} attempts" >&2
exit 1
