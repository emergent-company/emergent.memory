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
#   - timeline `review_requested`  -> requested_reviewer.login = Copilot, type = "Bot"
#   NEVER match bare "copilot".
#
#   Step A reads REQUEST signals (timeline `review_requested`, requested_reviewers),
#   where the timeline reports `Copilot` with `type: "Bot"`.
#   Step B polls ONLY REST GET /pulls/{n}/reviews, which returns just the two
#   `copilot-pull-request-reviewer*` spellings and never `Copilot`. Polling the
#   comments endpoint was considered and rejected: in every observed case
#   (#481, #483, #486) Copilot's review materialised as a submitted review on
#   /reviews with a correct `commit_id`, and /comments drags in ambiguous SHA
#   semantics (`commit_id` vs `original_commit_id`) for no benefit.
#   The two matchers are therefore deliberately NOT identical.
#
#   Copilot reviews return state COMMENTED, so do NOT require APPROVED — the
#   `commit_id == HEAD_SHA` staleness test is the single completion gate.
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

ERR_FILE="$(mktemp)"
trap 'rm -f "$ERR_FILE"' EXIT

# Bare `Copilot` is shared with the Copilot app identity; accept it only for a
# bot/app object. The live timeline exposes type="Bot" (verified on #481/#485/
# #486). If type is absent we cannot distinguish, so fall back to the login
# match rather than inventing a field.
is_bot_type() {
  case "${1,,}" in
    bot | app) return 0 ;;
    *) return 1 ;;
  esac
}

# Request signals (Step A): the timeline `review_requested` event reports
# `Copilot` with `type: "Bot"`, so this matcher accepts all three spellings
# (tightening `Copilot` to bot/app objects).
is_gated_request_login() {
  case "$1" in
    "copilot-pull-request-reviewer[bot]" | "copilot-pull-request-reviewer")
      return 0
      ;;
    "Copilot")
      if [ -z "${2:-}" ]; then
        return 0
      fi
      is_bot_type "$2" && return 0
      return 1
      ;;
    *)
      return 1
      ;;
  esac
}

# Review signals (Step B). Deliberately NARROWER than is_gated_request_login():
# this reads REST GET /pulls/{n}/reviews, which only ever returns the two
# `copilot-pull-request-reviewer*` spellings. `Copilot` appears solely on the
# comments endpoint and is excluded here. The two matchers differ by design —
# do not collapse them back into one.
is_gated_review_login() {
  case "$1" in
    "copilot-pull-request-reviewer[bot]" | "copilot-pull-request-reviewer")
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

# True when the row carries a literal TAB separator before we split on it.
has_tab() {
  case "$1" in
    *$'\t'*) return 0 ;;
    *) return 1 ;;
  esac
}

# `gh api --slurp` is mutually exclusive with `--jq` (gh 2.98.0), so the gate
# fetches the slurped pages and pipes them through `jq -r`. `add // []` flattens
# the page array and tolerates an empty result.
TIMELINE_JQ='(add // []) | .[] | select(.event=="review_requested") | (.requested_reviewer.login // "") + "\t" + (.requested_reviewer.type // "")'
REVIEWS_JQ='(add // []) | .[] | "\(.user.login)\t\(.commit_id)"'

# ---------------------------------------------------------------------------
# Step A — is a gated reviewer actually REQUESTED? PRs that never request a
# gated reviewer must not be delayed or blocked, so exit 0 immediately.
#
# Each source is judged independently: a failed fetch is NOT treated as "no
# reviewer requested". stderr is captured and logged so failures are diagnosable.
# ---------------------------------------------------------------------------
timeline_rows="$(timeout 30 gh api --paginate --slurp \
  "repos/$REPO/issues/$PR_NUMBER/timeline" \
  -H "Accept: application/vnd.github+json" \
  2>"$ERR_FILE" | jq -r "$TIMELINE_JQ")"
timeline_rc=$?
timeline_err="$(cat "$ERR_FILE")"

users_rows="$(timeout 30 gh api \
  "repos/$REPO/pulls/$PR_NUMBER/requested_reviewers" \
  2>"$ERR_FILE" | jq -r '.users[] | .login + "\t" + (.type // "")')"
users_rc=$?
users_err="$(cat "$ERR_FILE")"

timeline_ok=1
users_ok=1
if [ "$timeline_rc" -ne 0 ]; then
  timeline_ok=0
  log "WARNING: Step A source failed: timeline review_requested (rc=$timeline_rc): ${timeline_err:-<no stderr>}"
fi
if [ "$users_rc" -ne 0 ]; then
  users_ok=0
  log "WARNING: Step A source failed: requested_reviewers.users[] (rc=$users_rc): ${users_err:-<no stderr>}"
fi

if [ "$timeline_ok" -eq 0 ] && [ "$users_ok" -eq 0 ]; then
  log "FAIL-OPEN: every Step A source failed (timeline rc=$timeline_rc, requested_reviewers rc=$users_rc); cannot tell whether a gated reviewer was requested; not blocking"
  exit 0
fi

requested_source=""
if [ "$timeline_ok" -eq 1 ]; then
  while IFS= read -r row; do
    [ -n "$row" ] || continue
    if ! has_tab "$row"; then
      log "WARNING: Step A timeline row without a TAB separator, skipping: $row"
      continue
    fi
    login="${row%%$'\t'*}"
    rtype="${row#*$'\t'}"
    if is_gated_request_login "$login" "$rtype"; then
      requested_source="timeline review_requested event"
      break
    fi
  done <<<"$timeline_rows"
fi

if [ -z "$requested_source" ] && [ "$users_ok" -eq 1 ]; then
  while IFS= read -r row; do
    [ -n "$row" ] || continue
    if ! has_tab "$row"; then
      log "WARNING: Step A requested_reviewers row without a TAB separator, skipping: $row"
      continue
    fi
    login="${row%%$'\t'*}"
    rtype="${row#*$'\t'}"
    if is_gated_request_login "$login" "$rtype"; then
      requested_source="requested_reviewers.users[]"
      break
    fi
  done <<<"$users_rows"
fi

if [ -z "$requested_source" ]; then
  log "no gated reviewer requested on PR #$PR_NUMBER; exiting without waiting"
  exit 0
fi

log "gated reviewer requested on PR #$PR_NUMBER (via $requested_source); waiting for a review of head $HEAD_SHA"
log "requested_reviewers.users[] = [$(printf '%s' "$users_rows" | tr '\t' ' ' | paste -sd, -)]"

# ---------------------------------------------------------------------------
# Step B — poll GET /pulls/{n}/reviews until one from a gated reviewer covers
# HEAD_SHA. Step C is the fail-open deadline and the error path.
# ---------------------------------------------------------------------------
fail_open_deadline() {
  log "WARNING: timed out after ${MAX_WAIT_SECONDS}s waiting for a gated reviewer to review PR #$PR_NUMBER head $HEAD_SHA"
  log "FAIL-OPEN: exiting 0 so this required check reaches a terminal success state"
  exit 0
}

deadline=$(( $(date +%s) + MAX_WAIT_SECONDS ))
while :; do
  # Re-check the deadline immediately before each fetch so a slow call cannot
  # push us past the workflow timeout.
  now=$(date +%s)
  if [ "$now" -ge "$deadline" ]; then
    fail_open_deadline
  fi

  reviews="$(timeout 30 gh api --paginate --slurp \
    "repos/$REPO/pulls/$PR_NUMBER/reviews" \
    2>"$ERR_FILE" | jq -r "$REVIEWS_JQ")"
  reviews_rc=$?
  reviews_err="$(cat "$ERR_FILE")"

  if [ "$reviews_rc" -ne 0 ]; then
    log "FAIL-OPEN: error fetching reviews (rc=$reviews_rc): ${reviews_err:-<no stderr>}; not blocking"
    exit 0
  fi

  while IFS= read -r row; do
    [ -n "$row" ] || continue
    if ! has_tab "$row"; then
      log "WARNING: review row without a TAB separator, skipping: $row"
      continue
    fi
    login="${row%%$'\t'*}"
    commit_id="${row#*$'\t'}"
    if is_gated_review_login "$login" && [ "$commit_id" = "$HEAD_SHA" ]; then
      log "PASS: review by $login covers head $HEAD_SHA"
      exit 0
    fi
  done <<<"$reviews"

  now=$(date +%s)
  remaining=$(( deadline - now ))
  if [ "$remaining" -le 0 ]; then
    fail_open_deadline
  fi

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
