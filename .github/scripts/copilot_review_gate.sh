#!/usr/bin/env bash
#
# copilot_review_gate.sh — required check that waits until a requested reviewer
# bot has reviewed the CURRENT head SHA.
#
# Why: on PR #481 a human merged at 08:32:55, 7 seconds BEFORE GitHub Copilot's
# review comment landed at 08:33:02. This gate makes the `review-gate` required
# check wait until the requested bot submits a review whose `commit_id` equals
# the PR head SHA, so a large review cannot land after the merge.
#
# FAIL-OPEN BY DESIGN: a required check must always reach a terminal SUCCESS
# state. If Copilot is slow, down, or GitHub changes an undocumented API, we
# log a loud warning and exit 0. The alternative — hanging or failing — would
# block every PR forever on someone else's outage or on our own bug.
#
# Endpoint quirks (verified against the live API):
#   - REST GET /pulls/{n}/reviews  -> user.login = copilot-pull-request-reviewer[bot]
#   - REST GET /pulls/{n}/comments -> user.login = Copilot
#   - GraphQL / gh pr view --json reviews -> author.login = copilot-pull-request-reviewer
#   Match all three spellings; NEVER match bare "copilot".
#   Copilot reviews return state COMMENTED, so do NOT require APPROVED — the
#   `commit_id` staleness test is what matters.
#
# Runnable locally: set PR_NUMBER (and optionally HEAD_SHA/REPO); the gh CLI
# picks up your local auth automatically. In CI these come from the workflow.

set -uo pipefail

REPO="${GITHUB_REPOSITORY:-}"
if [ -z "$REPO" ]; then
  REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)"
fi
PR_NUMBER="${PR_NUMBER:-}"
HEAD_SHA="${HEAD_SHA:-}"
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-600}"
POLL_INTERVAL_SECONDS="${POLL_INTERVAL_SECONDS:-25}"

log() { printf '[review-gate] %s\n' "$*"; }

if [ -z "$REPO" ] || [ -z "$PR_NUMBER" ] || [ -z "$HEAD_SHA" ]; then
  log "FAIL-OPEN: missing REPO/PR_NUMBER/HEAD_SHA (REPO='$REPO' PR_NUMBER='$PR_NUMBER' HEAD_SHA='$HEAD_SHA'); nothing to gate"
  exit 0
fi

# Exact comparison against the known Copilot login spellings. The case patterns
# are quoted so the literal "[bot]" suffix is not treated as a glob.
is_gated_reviewer() {
  case "$1" in
    "copilot-pull-request-reviewer[bot]" | "copilot-pull-request-reviewer" | "Copilot")
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

# ---------------------------------------------------------------------------
# Step A — is a gated reviewer actually REQUESTED? PRs that never request a
# gated reviewer must not be delayed or blocked, so exit 0 immediately.
# ---------------------------------------------------------------------------
timeline_logins="$(gh api --paginate \
  "repos/$REPO/issues/$PR_NUMBER/timeline" \
  -H "Accept: application/vnd.github+json" \
  --jq '.[] | select(.event=="review_requested") | (.requested_reviewer.login // empty)' 2>/dev/null)"
timeline_rc=$?

users_logins="$(gh api \
  "repos/$REPO/pulls/$PR_NUMBER/requested_reviewers" \
  --jq '.users[].login' 2>/dev/null)"
users_rc=$?

if [ "$timeline_rc" -ne 0 ] && [ "$users_rc" -ne 0 ]; then
  log "FAIL-OPEN: could not read reviewer state (timeline rc=$timeline_rc, requested_reviewers rc=$users_rc); not blocking"
  exit 0
fi

requested_source=""
while IFS= read -r login; do
  [ -n "$login" ] || continue
  if is_gated_reviewer "$login"; then
    requested_source="timeline review_requested event"
    break
  fi
done <<<"$timeline_logins"

if [ -z "$requested_source" ]; then
  while IFS= read -r login; do
    [ -n "$login" ] || continue
    if is_gated_reviewer "$login"; then
      requested_source="requested_reviewers.users[]"
      break
    fi
  done <<<"$users_logins"
fi

if [ -z "$requested_source" ]; then
  log "no gated reviewer requested on PR #$PR_NUMBER; exiting without waiting"
  exit 0
fi

log "gated reviewer requested on PR #$PR_NUMBER (via $requested_source); waiting for a review of head $HEAD_SHA"
log "requested_reviewers.users[] = [$(printf '%s' "$users_logins" | paste -sd, -)]"

# Diagnostic ONLY. `copilot_work_started` is a live but UNDOCUMENTED timeline
# event; it is never used as a completion signal, just a hint in the log.
if gh api --paginate \
  "repos/$REPO/issues/$PR_NUMBER/timeline" \
  -H "Accept: application/vnd.github+json" \
  --jq '.[] | select(.event=="copilot_work_started") | .created_at' 2>/dev/null | grep -q .; then
  log "diagnostic: timeline reports copilot_work_started"
fi

# ---------------------------------------------------------------------------
# Step B — poll reviews until one from a gated reviewer covers HEAD_SHA.
# ---------------------------------------------------------------------------
deadline=$(( $(date +%s) + MAX_WAIT_SECONDS ))
while :; do
  reviews="$(gh api --paginate \
    "repos/$REPO/pulls/$PR_NUMBER/reviews" \
    --jq '.[] | "\(.user.login)\t\(.commit_id)"' 2>/dev/null)"
  reviews_rc=$?
  if [ "$reviews_rc" -ne 0 ]; then
    log "FAIL-OPEN: error fetching reviews (rc=$reviews_rc); not blocking"
    exit 0
  fi

  while IFS=$'\t' read -r login commit_id; do
    [ -n "${login:-}" ] || continue
    if is_gated_reviewer "$login" && [ "$commit_id" = "$HEAD_SHA" ]; then
      log "PASS: review by $login covers head $HEAD_SHA"
      exit 0
    fi
  done <<<"$reviews"

  now=$(date +%s)
  if [ "$now" -ge "$deadline" ]; then
    # ---------------------------------------------------------------------
    # Step C — deadline. Fail open: warn loudly, then exit 0 so the required
    # check always reaches a terminal success state. A Copilot outage or an
    # undocumented API change must not block every PR forever.
    # ---------------------------------------------------------------------
    log "WARNING: timed out after ${MAX_WAIT_SECONDS}s waiting for a gated reviewer to review PR #$PR_NUMBER head $HEAD_SHA"
    log "FAIL-OPEN: exiting 0 so this required check reaches a terminal success state"
    exit 0
  fi

  remaining=$(( deadline - now ))
  sleep_for="$POLL_INTERVAL_SECONDS"
  if [ "$sleep_for" -gt "$remaining" ]; then
    sleep_for="$remaining"
  fi
  if [ "$sleep_for" -lt 1 ]; then
    sleep_for=1
  fi
  log "waiting ${remaining}s for a gated reviewer to review $HEAD_SHA..."
  sleep "$sleep_for"
done
