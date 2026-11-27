#!/usr/bin/env bash
# Start the release path for a pull request the review gate just merged (#1047).
#
# WHY THIS EXISTS
# ---------------
# `github-review-gate.yml` arms auto-merge when the review bot approves, and
# GitHub then merges the PR as `app/github-actions`. That merge raises a push
# event from `GITHUB_TOKEN`, and GitHub starts **no workflow runs** for those,
# so `auto-tag.yml`'s `on: push: paths: ['VERSION']` trigger never fires: the
# tag is never created and the nine-workflow release fan-out never starts.
# v0.104.0's tag had to be pushed by hand because of exactly this.
#
# `workflow_dispatch` is a documented exception to that suppression, so the gate
# dispatches `auto-tag.yml` itself right after arming. The gate cannot dispatch
# blindly: a PR that does not touch `VERSION` must not start a release job, and
# a merge that outlives this helper's bounded wait is the scheduled
# `release-tag-watchdog.yml`'s job -- never a reason to fail a gate run for a PR
# that is already merged.
#
# The gate has the default branch on disk (`actions/checkout`), never the PR
# head, so this script runs reviewed code only.
#
# `auto-tag.yml` is idempotent (it exits early when the tag exists), so a
# dispatch for a merge this script cannot see is harmless: this helper and the
# watchdog may race without creating a second tag.
#
# Never exits non-zero for "no release needed" or "not merged yet" -- both are
# normal. It exits non-zero when the release state could not be read or the
# dispatch failed, so the gate's `|| echo warning` line is the signal.
#
# Usage:
#   dispatch-release-tag.sh <pr-number> [--repo OWNER/NAME] [--base main]
#                           [--workflow auto-tag.yml] [--wait SECONDS]
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: dispatch-release-tag.sh <pr-number> [--repo OWNER/NAME] [--base main]
                               [--workflow auto-tag.yml] [--wait SECONDS]
EOF
  exit 2
}

PR=""
REPO="${REPO:-${GITHUB_REPOSITORY:-}}"
BASE="main"
WORKFLOW="auto-tag.yml"
# Long enough for the queue to reach a reviewed PR, short enough that an
# `issue_comment` gate run does not sit on the runner: the gate job times out at
# 10 minutes and the merge may legitimately be waiting on other required checks.
WAIT=300

while [[ $# -gt 0 ]]; do
  case "$1" in
    --repo) REPO="$2"; shift 2 ;;
    --base) BASE="$2"; shift 2 ;;
    --workflow) WORKFLOW="$2"; shift 2 ;;
    --wait) WAIT="$2"; shift 2 ;;
    -h|--help) usage ;;
    -*) printf 'unknown option: %s\n' "$1" >&2; usage ;;
    *) PR="$1"; shift ;;
  esac
done

if [[ -z "$PR" ]]; then
  printf 'ERROR: a pull request number is required\n' >&2
  usage
fi
if [[ -z "$REPO" ]]; then
  printf 'ERROR: --repo OWNER/NAME (or $REPO) is required\n' >&2
  exit 2
fi
if ! [[ "$WAIT" =~ ^[0-9]+$ ]]; then
  printf 'ERROR: --wait takes a number of seconds\n' >&2
  exit 2
fi
for cmd in gh jq base64; do
  command -v "$cmd" >/dev/null 2>&1 || {
    printf 'ERROR: %s is required\n' "$cmd" >&2
    exit 2
  }
done

notice() { printf '::notice::%s\n' "$*"; }

# One `gh pr view` per poll: state, base branch and merge commit together, so
# the decision is made from a single consistent snapshot.
pr_state() {
  gh pr view "$PR" --repo "$REPO" --json state,baseRefName,mergeCommit \
    --jq '[.state, .baseRefName, (.mergeCommit.oid // "")] | @tsv'
}

deadline=$(( $(date +%s) + WAIT ))
while :; do
  snapshot=$(pr_state)
  IFS=$'\t' read -r state base_ref merge_sha <<<"$snapshot"

  if [[ "$state" == "MERGED" ]]; then
    break
  fi
  if [[ "$state" == "CLOSED" ]]; then
    notice "PR #${PR} is closed without a merge; no release to start."
    exit 0
  fi
  if (( $(date +%s) >= deadline )); then
    # The merge is still in flight. It is already armed, so it will land; the
    # scheduled release-tag-watchdog reconciles the tag after that. Failing here
    # would report a problem with a merge that has not happened yet.
    notice "PR #${PR} was still ${state} after ${WAIT}s; the merge is armed, so the scheduled release-tag-watchdog will reconcile the tag."
    exit 0
  fi
  sleep 5
done

if [[ "$base_ref" != "$BASE" ]]; then
  notice "PR #${PR} merged into ${base_ref}, not ${BASE}; the release path is for ${BASE} only."
  exit 0
fi

if [[ -z "$merge_sha" ]]; then
  printf 'ERROR: PR #%s is merged but reports no merge commit\n' "$PR" >&2
  exit 1
fi

# Did this merge actually change VERSION? The merge commit against its first
# parent is the definition of what the PR landed.
parent_sha=$(gh api "repos/${REPO}/commits/${merge_sha}" --jq '.parents[0].sha')
if [[ -z "$parent_sha" || "$parent_sha" == "null" ]]; then
  printf 'ERROR: merge commit %s has no parent\n' "$merge_sha" >&2
  exit 1
fi

# The contents API returns base64; `content` is newline-wrapped.
version_at() {
  gh api "repos/${REPO}/contents/VERSION?ref=$1" --jq '.content' 2>/dev/null \
    | tr -d ' \n\r' | base64 -d
}

if ! merged_version=$(version_at "$merge_sha"); then
  printf 'ERROR: could not read VERSION at merge commit %s\n' "$merge_sha" >&2
  exit 1
fi
if ! parent_version=$(version_at "$parent_sha"); then
  printf 'ERROR: could not read VERSION at %s\n' "$parent_sha" >&2
  exit 1
fi
merged_version=$(printf '%s' "$merged_version" | tr -d ' \n\r')
parent_version=$(printf '%s' "$parent_version" | tr -d ' \n\r')

if [[ "$merged_version" == "$parent_version" ]]; then
  notice "PR #${PR} merged as ${merge_sha:0:9} did not change VERSION (still ${merged_version}); no release to start."
  exit 0
fi

tag="v${merged_version}"
if gh api "repos/${REPO}/git/ref/tags/${tag}" >/dev/null 2>&1; then
  notice "PR #${PR} bumped VERSION to ${merged_version} and tag ${tag} already exists; nothing to do."
  exit 0
fi

echo "PR #${PR} merged as ${merge_sha:0:9} and bumped VERSION ${parent_version} -> ${merged_version}; dispatching ${WORKFLOW} on ${BASE}."
gh workflow run "$WORKFLOW" --repo "$REPO" --ref "$BASE"
echo "Dispatched ${WORKFLOW} on ${BASE} for ${tag}."
