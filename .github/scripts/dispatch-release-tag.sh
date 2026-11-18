#!/usr/bin/env bash
# Dispatch `auto-tag.yml` after the review gate's auto-merge lands (#1047).
#
# `github-review-gate.yml` arms auto-merge on a bot marker. GitHub completes
# that merge as `app/github-actions`, and the resulting push to `main` is raised
# by `GITHUB_TOKEN` — GitHub starts **no workflow runs** for it, so
# `auto-tag.yml`'s `on: push: paths: ['VERSION']` trigger never fires and the
# release tag (and therefore the whole fan-out) never happens. v0.104.0 needed a
# hand-pushed tag because of exactly this.
#
# The gate cannot observe a merge that happens after its own run ends, so this
# helper does the two things it *can* do: wait a bounded time for the merge it
# just armed, then dispatch `auto-tag.yml` on `main`. A dispatch-triggered run
# is one of the documented exceptions to the `GITHUB_TOKEN` suppression, so the
# tag is created and the fan-out starts. `auto-tag.yml` is idempotent — it exits
# early when the tag already exists — so a merge that carries no version bump,
# or a human recovery that beat us to it, dispatches a cheap no-op.
#
# Merges that outlive WAIT_SECONDS are not lost: the scheduled
# `release-tag-watchdog.yml` reconciles `VERSION` against the tag and dispatches
# auto-tag itself. This helper is the fast path; the watchdog is the guarantee.
#
# Only PRs targeting `main` are considered: a release is cut from `main` only,
# and a `dev` merge cannot change the tag.
#
# Usage:
#   dispatch-release-tag.sh <pr-number>
#
# Environment:
#   REPO (default $GITHUB_REPOSITORY), GH_TOKEN (both required by `gh`)
#   WAIT_SECONDS  how long to wait for the merge (default 300)
#   WAIT_INTERVAL seconds between state polls (default 15)
#
# Exit codes: 0 nothing to do / dispatched; 1 the dispatch failed; 2 usage.
set -euo pipefail

pr="${1:-}"
if [[ -z "$pr" ]]; then
  echo "usage: dispatch-release-tag.sh <pr-number>" >&2
  exit 2
fi

repo="${REPO:-${GITHUB_REPOSITORY:-}}"
if [[ -z "$repo" ]]; then
  echo "ERROR: REPO or GITHUB_REPOSITORY must be set." >&2
  exit 2
fi

wait_seconds="${WAIT_SECONDS:-300}"
interval="${WAIT_INTERVAL:-15}"
for value in "$wait_seconds" "$interval"; do
  if [[ ! "$value" =~ ^[0-9]+$ ]]; then
    echo "ERROR: WAIT_SECONDS and WAIT_INTERVAL must be whole seconds, got '$value'." >&2
    exit 2
  fi
done

base=$(gh pr view "$pr" --repo "$repo" --json baseRefName --jq '.baseRefName')
if [[ "$base" != "main" ]]; then
  echo "PR #${pr} targets ${base}, not main — releases are cut from main; not dispatching auto-tag"
  exit 0
fi

deadline=$((SECONDS + wait_seconds))
while :; do
  # `MERGED` is only reported once the merge commit exists on the base branch,
  # which is exactly when dispatching on `main` builds the tagged tree.
  state=$(gh pr view "$pr" --repo "$repo" --json state --jq '.state')
  case "$state" in
    MERGED) break ;;
    CLOSED)
      echo "PR #${pr} was closed without merging; nothing to tag"
      exit 0
      ;;
  esac
  if ((SECONDS >= deadline)); then
    echo "::notice::PR #${pr} has not merged after ${wait_seconds}s; the scheduled release-tag-watchdog will reconcile ${repo} so the tag is not lost (#1047)."
    exit 0
  fi
  sleep "$interval"
done

echo "PR #${pr} merged as the Actions app; dispatching auto-tag.yml on main so the release tag is created (#1047)"
if ! gh workflow run auto-tag.yml --repo "$repo" --ref main; then
  echo "ERROR: dispatching auto-tag.yml failed." >&2
  echo "The merge already happened and its push started no workflow run, so the tag" >&2
  echo "will not appear on its own:" >&2
  echo "  gh workflow run auto-tag.yml --repo ${repo} --ref main" >&2
  exit 1
fi
echo "Dispatched auto-tag.yml for PR #${pr}."
