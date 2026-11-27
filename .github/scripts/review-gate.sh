#!/usr/bin/env bash
# Decide what one review-marker comment does to the merge gate (#1385, #1047).
#
# WHY THIS IS A SCRIPT, NOT A `run:` BLOCK
# ----------------------------------------
# The job this replaced decided two things with `if` statements that were one
# comparison away from inverting, and one of them was inverted in production:
#
#   * **Arming is bot-only.** `github-review-gate.yml` exists because a review
#     comment cannot re-run `merge-guard` (it triggers on push/reopen), so the
#     gate dispatches the guard and arms auto-merge when the review App posts
#     `Verdict: MERGE`. A human's marker must not arm: a human who can post a
#     marker can run `scripts/merge-reviewed-pr.sh`, which applies the check and
#     marker gate the comment path does not.
#
#   * **Denying is anyone's.** Honouring a rejection grants nothing, so the
#     "never trusts a human-commenter" rule is not violated by it — and the old
#     author filter *was* the hole in #1385: product #1036 merged 2m12s after a
#     trusted `CHANGES_REQUESTED` on the same head, because the bridge ignored
#     every comment not authored by the review bot. A deny-only path for a
#     trusted human is the correction.
#
# Trust for the human half is the repository's own definition, read from the
# same places the merge guard reads it: `author_association` `OWNER`/
# `COLLABORATOR` (write access), or an immutable user id in
# `MERGE_GUARD_TRUSTED_USER_IDS` — `MEMBER` alone is org membership, which in
# these repos is what repository admins also report. A `Bot` is never trusted on
# either route, whatever a variable holds (#1290, #1310).
#
# Exit status: 0 for every decision, including "nothing to do". A non-zero exit
# would turn an ordinary comment into a red run, and the gate's own failure
# signal is the `merge-guard` check, not this job.
#
# Usage:
#   REPO=owner/name PR=42 BODY=... AUTHOR=... AUTHOR_ASSOC=... AUTHOR_ID=... \
#   [BOT_LOGINS=...] [BOT_LOGIN_LEGACY=...] [TRUSTED_IDS=...] \
#   .github/scripts/review-gate.sh
set -euo pipefail

REPO=${REPO:?REPO is required}
PR=${PR:?PR is required}
AUTHOR=${AUTHOR:?AUTHOR is required}
BODY=${BODY:-}
AUTHOR_ASSOC=${AUTHOR_ASSOC:-}
AUTHOR_ID=${AUTHOR_ID:-}
BOT_LOGINS=${BOT_LOGINS:-}
BOT_LOGIN_LEGACY=${BOT_LOGIN_LEGACY:-}
TRUSTED_IDS=${TRUSTED_IDS:-}

# A whitespace/comma-only list counts as unset, and the legacy singular name is
# read only then (the same fallback the gate always had).
logins="$BOT_LOGINS"
if [[ -z "${logins//[[:space:],]/}" ]]; then
  logins="$BOT_LOGIN_LEGACY"
fi

# GitHub logins are case-insensitive; ids are not.
login_in_list() { # <list> <login>
  local list=$1 who=$2
  [[ -n "${list//[[:space:],]/}" ]] || return 1
  printf '%s\n' "$list" | tr ', ' '\n\n' | tr -s '[:space:]' '\n' | sed '/^$/d' \
    | grep -Fxiq -- "$who"
}
id_in_list() { # <list> <id>
  local list=$1 id=$2
  [[ -n "$id" && -n "${list//[[:space:],]/}" ]] || return 1
  printf '%s\n' "$list" | tr ', ' '\n\n' | tr -s '[:space:]' '\n' | sed '/^$/d' \
    | grep -Fxq -- "$id"
}

is_bot=0
if login_in_list "$logins" "$AUTHOR"; then is_bot=1; fi

# A bot is never a trusted human, even when a variable lists its id: that
# misconfiguration is what made the gate delta-only twice (#1290, #1310).
is_trusted_human=0
if ((!is_bot)); then
  case "${AUTHOR_ASSOC^^}" in
    OWNER | COLLABORATOR) is_trusted_human=1 ;;
    *) id_in_list "$TRUSTED_IDS" "$AUTHOR_ID" && is_trusted_human=1 ;;
  esac
fi

if ((!is_bot && !is_trusted_human)); then
  echo "author ${AUTHOR} (${AUTHOR_ASSOC:-unknown}, id ${AUTHOR_ID:-unknown}) is neither a configured review bot nor a trusted identity; ignoring"
  exit 0
fi

# Marker parsing matches `scripts/lib/review-marker.sh` in the infra repo and the
# `merge-guard` workflow: a whole-line `Reviewed-SHA:` and one or more
# `Verdict:` lines, with any non-MERGE verdict rejecting. `grep`/`sed` rather
# than `awk` intervals: mawk (the default `awk` on some runners) has no `{40}`,
# which silently turns every marker into "unmarked".
sha=$(printf '%s\n' "$BODY" \
  | grep -E '^Reviewed-SHA: [0-9a-f]{40}[[:space:]]*$' \
  | tail -n 1 \
  | sed -E 's/^Reviewed-SHA: ([0-9a-f]{40}).*$/\1/') || true
verdicts=$(printf '%s\n' "$BODY" \
  | sed -nE 's/^Verdict: ([A-Za-z_][A-Za-z_ ]*)[[:space:]]*$/\1/p') || true
if [[ -z "$sha" || -z "$verdicts" ]]; then
  echo "comment carries no Reviewed-SHA/Verdict marker; ignoring"
  exit 0
fi
verdict=MERGE
if printf '%s\n' "$verdicts" | grep -qv '^MERGE$'; then
  verdict=CHANGES_REQUESTED
fi

# A marker for another commit must never act on this one: the head may have
# moved since the review.
head=$(gh pr view "$PR" --repo "$REPO" --json headRefOid --jq '.headRefOid')
if [[ "$sha" != "$head" ]]; then
  echo "marker ${sha} does not name the head ${head}; ignoring"
  exit 0
fi

case "$verdict" in
  MERGE)
    if ((!is_bot)); then
      echo "a trusted human MERGE marker does not arm auto-merge: run scripts/merge-reviewed-pr.sh ${PR} (or its --arm) so the check list and marker gate are applied by the wrapper"
      exit 0
    fi
    # The guard is re-dispatched so the head gets a fresh verdict run (the
    # comment did not move the head, so no pull_request_target event exists).
    gh workflow run merge-guard.yml --repo "$REPO" -f pr="$PR" \
      || echo "warning: could not dispatch merge-guard"
    gh pr merge "$PR" --repo "$REPO" --auto --squash \
      || echo "warning: could not enable auto-merge"
    # The armed merge is completed by the platform as `app/github-actions`, whose
    # GITHUB_TOKEN push starts no workflow runs -- so `auto-tag.yml` never fires
    # and a release bump lands untagged (#1363). The dispatch is exempt from that
    # suppression and is idempotent; it does nothing for a PR that did not touch
    # VERSION. 300s is most of this job's 10-minute budget and covers the usual
    # case (a marker is posted once the checks are green, so the merge lands in
    # seconds); a merge still held past it is reconciled by the scheduled
    # `release-tag-watchdog.yml`, which is why the helper exits 0 rather than red.
    bash "${DISPATCH_RELEASE:-$(dirname "${BASH_SOURCE[0]}")/dispatch-release-tag.sh}" "$PR" \
      --repo "$REPO" --wait 300 \
      || echo "warning: could not start the release path for a VERSION bump"
    ;;
  CHANGES_REQUESTED)
    # Deny-only, and immediate: dispatch the guard so a previously-green verify
    # cannot stand after a rejection on an unchanged head, then disarm. This is
    # the path that did not exist when #1036 landed 2m12s after a rejection.
    gh workflow run merge-guard.yml --repo "$REPO" -f pr="$PR" \
      || echo "warning: could not dispatch merge-guard"
    gh pr merge "$PR" --repo "$REPO" --disable-auto || true
    ;;
esac
