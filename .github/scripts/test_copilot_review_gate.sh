#!/usr/bin/env bash
#
# test_copilot_review_gate.sh — plain-bash tests for copilot_review_gate.sh.
#
# No bats dependency: `gh` is stubbed on PATH with a fake that serves canned
# fixtures, and the real gate script is run against each scenario. Requires
# `jq` (the gate itself pipes gh output through jq, as it does in CI).
#
# Usage: bash .github/scripts/test_copilot_review_gate.sh

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATE="$HERE/copilot_review_gate.sh"
HEAD_SHA="HEAD_SHA_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
BIN="$WORKDIR/bin"
FIX="$WORKDIR/fixtures"
mkdir -p "$BIN" "$FIX"

# ---------------------------------------------------------------------------
# Fake `gh`. The gate pipes gh output into jq itself (gh --slurp is mutually
# exclusive with --jq), so the fake simply cats the fixture for the requested
# endpoint. A `<kind>.fail` file makes the call fail with that stderr text.
# ---------------------------------------------------------------------------
cat > "$BIN/gh" <<'FAKE_GH'
#!/usr/bin/env bash
set -uo pipefail
endpoint=""
for a in "$@"; do
  case "$a" in
    repos/*) endpoint="$a" ;;
  esac
done

case "$endpoint" in
  *issues/*/timeline) kind="timeline" ;;
  */requested_reviewers) kind="requested" ;;
  */reviews) kind="reviews" ;;
  *) echo "fake gh: unknown endpoint '${endpoint}'" >&2; exit 2 ;;
esac

if [ -f "$FAKE_GH_FIXTURES/$kind.fail" ]; then
  cat "$FAKE_GH_FIXTURES/$kind.fail" >&2
  exit 1
fi

if [ ! -f "$FAKE_GH_FIXTURES/$kind.json" ]; then
  echo "fake gh: missing fixture $FAKE_GH_FIXTURES/$kind.json" >&2
  exit 1
fi

cat "$FAKE_GH_FIXTURES/$kind.json"
FAKE_GH
chmod +x "$BIN/gh"

# ---------------------------------------------------------------------------
# Harness
# ---------------------------------------------------------------------------
PASSED=0
FAILED=0
OUT=""
RC=0

pass() { PASSED=$((PASSED + 1)); printf 'PASS: %s\n' "$1"; }
fail() {
  FAILED=$((FAILED + 1))
  printf 'FAIL: %s\n' "$1"
  if [ -n "$OUT" ]; then printf '%s\n' "$OUT" | sed 's/^/    | /'; fi
}

reset_fixtures() { rm -f "$FIX"/*.json "$FIX"/*.fail; }
set_fixture() { printf '%s' "$2" > "$FIX/$1.json"; }
fail_fixture() { printf '%s' "$2" > "$FIX/$1.fail"; }

run_gate() { # <max_wait> <poll>
  OUT="$(PATH="$BIN:$PATH" FAKE_GH_FIXTURES="$FIX" GITHUB_REPOSITORY="test/repo" \
      PR_NUMBER=1 HEAD_SHA="$HEAD_SHA" MAX_WAIT_SECONDS="$1" POLL_INTERVAL_SECONDS="$2" \
      bash "$GATE" 2>&1)"
  RC=$?
}

assert_rc0() {
  if [ "$RC" -eq 0 ]; then pass "$1 (exit 0)"; else fail "$1 (exit $RC, expected 0)"; fi
}
assert_contains() {
  case "$OUT" in *"$2"*) pass "$1" ;; *) fail "$1 (missing: $2)" ;; esac
}
assert_not_contains() {
  case "$OUT" in *"$2"*) fail "$1 (unexpected: $2)" ;; *) pass "$1" ;; esac
}

COPILOT_BOT_REQUEST='[{"event":"review_requested","requested_reviewer":{"login":"Copilot","type":"Bot"}}]'
NO_REQUEST='[{"event":"review_requested","requested_reviewer":{"login":"someuser","type":"User"}}]'
EMPTY_USERS='{"users":[],"teams":[]}'

# ---------------------------------------------------------------------------
# (a) no gated reviewer requested -> exit 0 immediately, no polling
# ---------------------------------------------------------------------------
reset_fixtures
set_fixture timeline "[$NO_REQUEST]"
set_fixture requested "$EMPTY_USERS"
run_gate 2 1
assert_rc0 "a: no gated reviewer requested"
assert_contains "a: logs 'no gated reviewer requested'" "no gated reviewer requested"
assert_not_contains "a: does not poll" "for a gated reviewer to review"
assert_not_contains "a: no PASS" "PASS"

# ---------------------------------------------------------------------------
# (a2) bare `Copilot` with a non-bot type is NOT a gated request
# ---------------------------------------------------------------------------
reset_fixtures
set_fixture timeline '[[{"event":"review_requested","requested_reviewer":{"login":"Copilot","type":"User"}}]]'
set_fixture requested '{"users":[{"login":"Copilot","type":"User"}],"teams":[]}'
run_gate 2 1
assert_rc0 "a2: Copilot with type=User is not gated"
assert_contains "a2: logs 'no gated reviewer requested'" "no gated reviewer requested"

# ---------------------------------------------------------------------------
# (b) requested + only a stale-SHA review -> poll to deadline, fail-open, no PASS
# ---------------------------------------------------------------------------
reset_fixtures
set_fixture timeline "[$COPILOT_BOT_REQUEST]"
set_fixture requested "$EMPTY_USERS"
set_fixture reviews '[[{"user":{"login":"copilot-pull-request-reviewer[bot]"},"commit_id":"STALE_SHA"}]]'
run_gate 2 1
assert_rc0 "b: stale SHA fails open"
assert_contains "b: logs timeout warning" "WARNING: timed out"
assert_contains "b: logs FAIL-OPEN" "FAIL-OPEN"
assert_not_contains "b: no PASS" "PASS"

# ---------------------------------------------------------------------------
# (c) requested + a review for HEAD_SHA -> PASS
# ---------------------------------------------------------------------------
reset_fixtures
set_fixture timeline "[$COPILOT_BOT_REQUEST]"
set_fixture requested "$EMPTY_USERS"
set_fixture reviews "[[{\"user\":{\"login\":\"copilot-pull-request-reviewer[bot]\"},\"commit_id\":\"$HEAD_SHA\"}]]"
run_gate 5 1
assert_rc0 "c: matching SHA passes"
assert_contains "c: logs PASS" "PASS: review by copilot-pull-request-reviewer[bot] covers head $HEAD_SHA"
assert_not_contains "c: no timeout" "WARNING: timed out"

# ---------------------------------------------------------------------------
# (c2) request seen only via requested_reviewers (timeline empty) -> still gates
# ---------------------------------------------------------------------------
reset_fixtures
set_fixture timeline "[[ ]]"
set_fixture requested '{"users":[{"login":"Copilot","type":"Bot"}],"teams":[]}'
set_fixture reviews "[[{\"user\":{\"login\":\"copilot-pull-request-reviewer\"},\"commit_id\":\"$HEAD_SHA\"}]]"
run_gate 5 1
assert_rc0 "c2: requested_reviewers source gates"
assert_contains "c2: logs PASS via review spelling" "PASS: review by copilot-pull-request-reviewer covers head $HEAD_SHA"

# ---------------------------------------------------------------------------
# (d) every Step A source errors -> fail-open with explicit warning
# ---------------------------------------------------------------------------
reset_fixtures
fail_fixture timeline "timeline boom"
fail_fixture requested "requested boom"
set_fixture reviews "[[ ]]"
run_gate 2 1
assert_rc0 "d: all Step A sources failing still exits 0"
assert_contains "d: warns each source failed" "Step A source failed: timeline review_requested"
assert_contains "d: warns each source failed" "Step A source failed: requested_reviewers.users[]"
assert_contains "d: loud fail-open warning" "every Step A source failed"
assert_not_contains "d: not reported as 'no gated reviewer'" "no gated reviewer requested"

# ---------------------------------------------------------------------------
# (d2) partial Step A failure is NOT treated as "no reviewer requested"
# ---------------------------------------------------------------------------
reset_fixtures
fail_fixture timeline "timeline boom"
set_fixture requested '{"users":[{"login":"Copilot","type":"Bot"}],"teams":[]}'
set_fixture reviews "[[{\"user\":{\"login\":\"copilot-pull-request-reviewer[bot]\"},\"commit_id\":\"$HEAD_SHA\"}]]"
run_gate 5 1
assert_rc0 "d2: partial Step A failure still gates"
assert_contains "d2: logs the failed source" "Step A source failed: timeline review_requested"
assert_contains "d2: still passes" "PASS: review by copilot-pull-request-reviewer[bot]"

# ---------------------------------------------------------------------------
# (e1) bare `Copilot` in the reviews source does NOT satisfy Step B
# ---------------------------------------------------------------------------
reset_fixtures
set_fixture timeline "[$COPILOT_BOT_REQUEST]"
set_fixture requested "$EMPTY_USERS"
set_fixture reviews "[[{\"user\":{\"login\":\"Copilot\"},\"commit_id\":\"$HEAD_SHA\"}]]"
run_gate 2 1
assert_rc0 "e1: bare Copilot review spelling fails open"
assert_not_contains "e1: no PASS" "PASS"
assert_contains "e1: times out" "WARNING: timed out"

# ---------------------------------------------------------------------------
# (e2) unrelated logins do NOT satisfy Step B
# ---------------------------------------------------------------------------
reset_fixtures
set_fixture timeline "[$COPILOT_BOT_REQUEST]"
set_fixture requested "$EMPTY_USERS"
set_fixture reviews "[[{\"user\":{\"login\":\"emergent-code-reviewer[bot]\"},\"commit_id\":\"$HEAD_SHA\"},{\"user\":{\"login\":\"someuser\"},\"commit_id\":\"$HEAD_SHA\"}]]"
run_gate 2 1
assert_rc0 "e2: unrelated reviewers fail open"
assert_not_contains "e2: no PASS" "PASS"
assert_contains "e2: times out" "WARNING: timed out"

# ---------------------------------------------------------------------------
printf '\n────────────────────────────────────────\n'
printf 'review-gate tests: %d passed, %d failed\n' "$PASSED" "$FAILED"
if [ "$FAILED" -ne 0 ]; then
  exit 1
fi
