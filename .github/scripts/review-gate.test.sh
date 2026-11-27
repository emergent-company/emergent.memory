#!/usr/bin/env bash
# Hermetic regression suite for review-gate.sh (#1385).
#
# WHY: this script decides whether a comment arms, disarms or is ignored, and
# every mistake is silent in the wrong direction. The one that happened in
# production is the rejection path: product #1036 merged 2m12s after a trusted
# `CHANGES_REQUESTED` on the same head, because the gate ignored every comment
# that was not authored by the review bot. The opposite mistake -- arming on a
# human's marker -- skips the wrapper's check list and marker gate.
#
# `bash -n` and a YAML parse catch none of that, so the suite stubs `gh` on PATH
# and asserts the exact commands run, or that none ran:
#
#   * a bot MERGE arms, dispatches the guard and starts the release path;
#   * a bot rejection disarms and re-dispatches the guard;
#   * a *trusted human* rejection disarms -- the fix itself (#1385);
#   * a MEMBER is trusted only through the id allowlist;
#   * a human MERGE marker never arms;
#   * a Bot is never trusted, even with its id in the allowlist;
#   * a stale marker or an unmarked comment does nothing.
#
# `DISPATCH_RELEASE` points at a recording stub, so the release helper is not
# exercised here (it has its own suite, `dispatch-release-tag.test.sh`).
#
# Offline, deterministic, no credentials.
#
# Usage:
#   .github/scripts/review-gate.test.sh
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/review-gate.sh"
if [[ ! -f $script ]]; then
  printf 'FAIL [review-gate]: script not found at %s\n' "$script" >&2
  exit 1
fi

REPO=emergent-company/emergent.memory
PR=1036
HEAD=ef8ab99b8aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
STALE=0123456789abcdef0123456789abcdef01234567
BOT='memory-review-memory-coding-7eaf[bot]'
ALLOW=1051101,4037961,188950

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
bin="$tmp/bin"
state="$tmp/state"
mkdir -p "$bin" "$state"

cat >"$bin/gh" <<'STUB'
#!/usr/bin/env bash
# Deterministic `gh` stand-in: records every call, answers the head read.
set -euo pipefail
: "${GH_STUB_STATE:?}"
printf '%s\n' "$*" >>"${GH_STUB_STATE}/calls"
if [[ "${1:-} ${2:-}" == "pr view" && "$*" == *"--json headRefOid"* ]]; then
  printf '%s\n' "${GH_STUB_HEAD:?}"
fi
exit 0
STUB

cat >"$bin/release-stub" <<'STUB'
#!/usr/bin/env bash
# Records the release dispatch the gate asks for.
printf 'release %s\n' "$*" >>"${GH_STUB_STATE}/calls"
STUB
chmod +x "$bin/gh" "$bin/release-stub"

fail=0
n=0
ok() { n=$((n + 1)); printf 'ok [review-gate]: %s\n' "$1"; }
bad() {
  n=$((n + 1))
  printf 'FAIL [review-gate]: %s\n' "$1" >&2
  sed 's/^/    call: /' "${state}/calls" >&2 || true
  fail=1
}

status=0
run_case() { # <author> <assoc> <id> <body> [ENV=value...]
  local author=$1 assoc=$2 id=$3 body=$4
  shift 4
  : >"${state}/calls"
  status=0
  env -u BOT_LOGINS -u BOT_LOGIN_LEGACY -u TRUSTED_IDS \
    PATH="${bin}:${PATH}" \
    GH_STUB_STATE="${state}" GH_STUB_HEAD="${HEAD}" \
    REPO="${REPO}" PR="${PR}" AUTHOR="${author}" AUTHOR_ASSOC="${assoc}" \
    AUTHOR_ID="${id}" BODY="${body}" \
    DISPATCH_RELEASE="${bin}/release-stub" \
    "$@" \
    bash "$script" >"${tmp}/out" 2>"${tmp}/err" || status=$?
}

armed() { grep -qxF "pr merge ${PR} --repo ${REPO} --auto --squash" "${state}/calls"; }
disarmed() { grep -qxF "pr merge ${PR} --repo ${REPO} --disable-auto" "${state}/calls"; }
guard_dispatched() { grep -qxF "workflow run merge-guard.yml --repo ${REPO} -f pr=${PR}" "${state}/calls"; }
release_started() { grep -q '^release ' "${state}/calls"; }
no_action() { ! grep -qE '^(pr merge|workflow run|release)' "${state}/calls"; }

comment() { # <sha> <verdict...>
  local sha=$1
  shift
  printf 'Reviewed-SHA: %s\n' "$sha"
  local v
  for v in "$@"; do printf 'Verdict: %s\n' "$v"; done
}

# 1. A bot MERGE arms, dispatches the guard and starts the release path.
run_case "$BOT" MEMBER 357094646 "$(comment "$HEAD" MERGE)" \
  BOT_LOGINS="$BOT" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || ! armed || ! guard_dispatched || ! release_started || disarmed; then
  bad "a bot MERGE arms, dispatches the guard and starts the release path (exit ${status})"
else
  ok "a bot MERGE arms, dispatches the guard and starts the release path"
fi

# 2. A bot rejection disarms.
run_case "$BOT" MEMBER 357094646 "$(comment "$HEAD" CHANGES_REQUESTED)" \
  BOT_LOGINS="$BOT" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || armed || ! disarmed || ! guard_dispatched; then
  bad "a bot rejection disarms and re-evaluates the guard (exit ${status})"
else
  ok "a bot rejection disarms and re-evaluates the guard"
fi

# 3. THE FIX (#1385): a trusted human rejection disarms.
run_case mkucharz OWNER 1051101 "$(comment "$HEAD" CHANGES_REQUESTED)" \
  BOT_LOGINS="$BOT" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || armed || ! disarmed || ! guard_dispatched; then
  bad "a trusted human (OWNER) rejection disarms (#1385) (exit ${status})"
else
  ok "a trusted human (OWNER) rejection disarms (#1385)"
fi

# 4. ... and does not depend on the bot list being configured at all.
run_case mkucharz COLLABORATOR 4037961 "$(comment "$HEAD" CHANGES_REQUESTED)"
if ((status != 0)) || armed || ! disarmed; then
  bad "a trusted rejection works with REVIEW_BOT_LOGINS unset (exit ${status})"
else
  ok "a trusted rejection works with REVIEW_BOT_LOGINS unset"
fi

# 5. A MEMBER is trusted only through the id allowlist.
run_case someone MEMBER 188950 "$(comment "$HEAD" CHANGES_REQUESTED)" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || ! disarmed; then
  bad "a MEMBER in the id allowlist is trusted to deny (exit ${status})"
else
  ok "a MEMBER in the id allowlist is trusted to deny"
fi

# 6. ... and not otherwise.
run_case someone MEMBER 999999 "$(comment "$HEAD" CHANGES_REQUESTED)" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || ! no_action; then
  bad "a MEMBER outside the allowlist is ignored (exit ${status})"
else
  ok "a MEMBER outside the allowlist is ignored"
fi

# 7. A fork contributor is ignored.
run_case outside-user CONTRIBUTOR 1234 "$(comment "$HEAD" CHANGES_REQUESTED)" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || ! no_action; then
  bad "a CONTRIBUTOR is ignored (exit ${status})"
else
  ok "a CONTRIBUTOR is ignored"
fi

# 8. A human MERGE marker never arms: the wrapper owns that path.
run_case mkucharz OWNER 1051101 "$(comment "$HEAD" MERGE)" \
  BOT_LOGINS="$BOT" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || ! no_action; then
  bad "a human MERGE marker does not arm (exit ${status})"
else
  ok "a human MERGE marker does not arm"
fi

# 9. An id in the allowlist is never an *arming* route (#1290, #1310). A review
#    App's id was once in MERGE_GUARD_TRUSTED_USER_IDS; the policy is that an
#    allowlisted identity may deny, never arm. Read with the bot list that does
#    not name it, so the id allowlist is the only thing that could grant trust.
run_case reviewer-bot OWNER 357094646 "$(comment "$HEAD" MERGE)" \
  BOT_LOGINS=other-bot[bot] TRUSTED_IDS=357094646
if ((status != 0)) || armed || release_started; then
  bad "an allowlisted identity does not arm (exit ${status})"
else
  ok "an allowlisted identity does not arm"
fi

# 10. A stale marker acts on nothing.
run_case mkucharz OWNER 1051101 "$(comment "$STALE" CHANGES_REQUESTED)" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || ! no_action; then
  bad "a marker for another head is ignored (exit ${status})"
else
  ok "a marker for another head is ignored"
fi

# 11. A comment with no marker does nothing.
run_case mkucharz OWNER 1051101 "please fix the thing" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || ! no_action; then
  bad "an unmarked comment does nothing (exit ${status})"
else
  ok "an unmarked comment does nothing"
fi

# 12. Any non-MERGE verdict rejects, matching the guard: a quoted approval
#     template cannot approve over a rejection.
run_case "$BOT" MEMBER 357094646 "$(comment "$HEAD" MERGE CHANGES_REQUESTED)" BOT_LOGINS="$BOT"
if ((status != 0)) || armed || ! disarmed; then
  bad "a comment carrying MERGE and a rejection rejects (exit ${status})"
else
  ok "a comment carrying MERGE and a rejection rejects"
fi

# 13. Trailing whitespace after the verdict must not turn an approval into a
#     rejection (#1206 review). The first cut's greedy capture absorbed it,
#     `^MERGE$` then failed, and the gate disarmed a bot approval.
run_case "$BOT" MEMBER 357094646 "$(printf 'Reviewed-SHA: %s\nVerdict: MERGE   \n' "$HEAD")" \
  BOT_LOGINS="$BOT" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || ! armed || disarmed; then
  bad "a verdict with trailing whitespace still arms (exit ${status})"
else
  ok "a verdict with trailing whitespace still arms"
fi

# 14. A padded SHA line still matches, and a multi-word verdict still rejects --
#     the class the old awk allowed, with its tail trimmed rather than captured.
run_case "$BOT" MEMBER 357094646 "$(printf 'Reviewed-SHA: %s  \nVerdict: CHANGES REQUESTED\n' "$HEAD")" \
  BOT_LOGINS="$BOT" TRUSTED_IDS="$ALLOW"
if ((status != 0)) || armed || ! disarmed; then
  bad "a multi-word verdict with a padded SHA line rejects (exit ${status})"
else
  ok "a multi-word verdict with a padded SHA line rejects"
fi

if ((fail)); then
  printf 'review-gate_test: FAILED\n' >&2
  exit 1
fi
printf 'review-gate_test: passed (%d cases)\n' "$n"
