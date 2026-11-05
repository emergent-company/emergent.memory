#!/bin/sh
# Hermetic test for deploy/self-hosted/scripts/retry.sh (#83).
#
# retry.sh was added to ride out transient network faults in image builds. Its
# failure modes are silent: sleeping after the last attempt only makes a build
# slower, swallowing the command's status reports success after a real failure,
# and accepting a missing command turns a Dockerfile typo into a skipped build
# step. Each of those is pinned below with a stub command and a stub `sleep`.
#
# The helper runs under POSIX sh -- busybox ash in the builder image and dash on
# Debian/Ubuntu -- so this test is POSIX sh too. Run it with either:
#
#   sh deploy/self-hosted/scripts/retry_test.sh
#   busybox ash deploy/self-hosted/scripts/retry_test.sh
#
# To exercise the helper under a specific shell, set RETRY_SH (word-split, so a
# two-word value is allowed):
#
#   RETRY_SH="busybox ash" sh deploy/self-hosted/scripts/retry_test.sh
#
# No network, no docker, no sleeping: the `sleep` on PATH is a stub that only
# records its arguments.
#
# RETRY_SH may hold two words ("busybox ash"), so it is deliberately word-split
# at each use.
# shellcheck disable=SC2086
set -u

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
RETRY="$SCRIPT_DIR/retry.sh"
RETRY_SH=${RETRY_SH:-sh}

if [ ! -f "$RETRY" ]; then
  echo "retry_test.sh: cannot find retry.sh next to this test ($RETRY)" >&2
  exit 1
fi

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/retry_test.XXXXXX") || exit 1
trap 'rm -rf "$TMP_ROOT"' EXIT HUP INT TERM

checks=0
failures=0

pass() { checks=$((checks + 1)); printf 'PASS: %s\n' "$1"; }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'FAIL: %s\n' "$1" >&2; }

check_eq() { # check_eq <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    pass "$1"
  else
    fail "$1 (want '$2', got '$3')"
  fi
}

check_contains() { # check_contains <label> <needle> <haystack>
  case "$3" in
    *"$2"*) pass "$1" ;;
    *) fail "$1 (missing '$2' in '$3')" ;;
  esac
}

check_not_contains() { # check_not_contains <label> <needle> <haystack>
  case "$3" in
    *"$2"*) fail "$1 (unexpected '$2' in '$3')" ;;
    *) pass "$1" ;;
  esac
}

check_count() { # check_count <label> <expected> <needle> <haystack>
  actual=$(printf '%s\n' "$4" | grep -F -c -- "$3")
  check_eq "$1" "$2" "$actual"
}

count_lines() { # count_lines <file>
  if [ -f "$1" ]; then
    wc -l <"$1" | tr -d ' '
  else
    printf '0\n'
  fi
}

# The fake command overwrites its counter with the invocation number, so the
# file's content is the number of runs.
read_counter() { # read_counter <file>
  if [ -f "$1" ]; then
    cat "$1"
  else
    printf '0\n'
  fi
}

# The command under test: counts its invocations, emits one stdout and one
# stderr line tagged with that count, then exits with $FAKE_STATUS.
FAKE="$TMP_ROOT/fake-command"
cat >"$FAKE" <<'FAKE_EOF'
#!/bin/sh
n=0
if [ -f "${FAKE_COUNT}" ]; then
  n=$(cat "${FAKE_COUNT}")
fi
n=$((n + 1))
printf '%s\n' "$n" >"${FAKE_COUNT}"
printf 'stdout-attempt-%s\n' "$n"
printf 'stderr-attempt-%s\n' "$n" >&2
exit "${FAKE_STATUS:-0}"
FAKE_EOF
chmod +x "$FAKE"

# A `sleep` that never sleeps, only records that it was asked to.
mkdir -p "$TMP_ROOT/bin"
SLEEP_STUB="$TMP_ROOT/bin/sleep"
cat >"$SLEEP_STUB" <<'SLEEP_EOF'
#!/bin/sh
printf '%s\n' "$*" >>"${SLEEP_LOG}"
exit 0
SLEEP_EOF
chmod +x "$SLEEP_STUB"

run_retry() { # run_retry <count-file> <fake-status> <retry args...>
  retry_count=$1
  retry_status=$2
  shift 2
  env FAKE_COUNT="$retry_count" FAKE_STATUS="$retry_status" \
    $RETRY_SH "$RETRY" "$@"
}

run_retry_stubbed_sleep() { # run_retry_stubbed_sleep <count-file> <status> <sleep-log> <retry args...>
  retry_count=$1
  retry_status=$2
  retry_sleep_log=$3
  shift 3
  env PATH="$TMP_ROOT/bin:$PATH" SLEEP_LOG="$retry_sleep_log" \
    FAKE_COUNT="$retry_count" FAKE_STATUS="$retry_status" \
    $RETRY_SH "$RETRY" "$@"
}

# --- bounded attempts, status preserved, output printed once ----------------
count_file="$TMP_ROOT/count-bounded"
out=$(run_retry "$count_file" 7 3 0 "bounded" -- "$FAKE" 2>&1)
rc=$?
check_eq 'bounded: runs exactly 3 attempts (not 4)' '3' "$(read_counter "$count_file")"
check_eq 'bounded: exits with the command status (7)' '7' "$rc"
check_contains 'bounded: reports exhaustion' 'failed after 3 attempt(s)' "$out"
check_count 'bounded: final attempt output printed once' '1' 'stdout-attempt-3' "$out"
check_count 'bounded: final attempt stderr printed once' '1' 'stderr-attempt-3' "$out"
check_not_contains 'bounded: first attempt output not replayed' 'stdout-attempt-1' "$out"
check_not_contains 'bounded: second attempt stderr not replayed' 'stderr-attempt-2' "$out"
check_not_contains 'bounded: no retry after the final attempt' 'attempt 3/3' "$out"

# --- success runs the command once and passes its output through ------------
count_file="$TMP_ROOT/count-success"
out=$(run_retry "$count_file" 0 3 0 "success" -- "$FAKE" 2>&1)
rc=$?
check_eq 'success: runs exactly 1 attempt' '1' "$(read_counter "$count_file")"
check_eq 'success: exits 0' '0' "$rc"
check_count 'success: output printed once' '1' 'stdout-attempt-1' "$out"
check_not_contains 'success: no exhaustion message' 'failed after' "$out"

# --- a command that succeeds on the second attempt -------------------------
# FAKE_STATUS is fixed per run, so simulate the transient with a wrapper.
flaky="$TMP_ROOT/flaky"
cat >"$flaky" <<'FLAKY_EOF'
#!/bin/sh
n=0
if [ -f "${FAKE_COUNT}" ]; then
  n=$(cat "${FAKE_COUNT}")
fi
n=$((n + 1))
printf '%s\n' "$n" >"${FAKE_COUNT}"
printf 'flaky-output-%s\n' "$n"
[ "$n" -ge 2 ] && exit 0
exit 5
FLAKY_EOF
chmod +x "$flaky"
count_file="$TMP_ROOT/count-transient"
out=$(env FAKE_COUNT="$count_file" $RETRY_SH "$RETRY" 3 0 "transient" -- "$flaky" 2>&1)
rc=$?
check_eq 'transient: retries until success' '2' "$(read_counter "$count_file")"
check_eq 'transient: exits 0 once the command succeeds' '0' "$rc"
check_count 'transient: final output printed once' '1' 'flaky-output-2' "$out"
check_not_contains 'transient: failed attempt output suppressed' 'flaky-output-1' "$out"

# --- no sleep after the final attempt --------------------------------------
count_file="$TMP_ROOT/count-sleep"
sleep_log="$TMP_ROOT/sleep.log"
out=$(run_retry_stubbed_sleep "$count_file" 9 "$sleep_log" 2 1 "sleepy" -- "$FAKE" 2>&1)
rc=$?
check_eq 'sleep: two attempts run' '2' "$(read_counter "$count_file")"
check_eq 'sleep: one sleep, between attempts' '1' "$(count_lines "$sleep_log")"
check_contains 'sleep: used the requested delay' 'retrying in 1s' "$out"
check_eq 'sleep: command status preserved' '9' "$rc"

count_file="$TMP_ROOT/count-single"
sleep_log="$TMP_ROOT/sleep-single.log"
out=$(run_retry_stubbed_sleep "$count_file" 9 "$sleep_log" 1 5 "single" -- "$FAKE" 2>&1)
rc=$?
check_eq 'single attempt: runs once' '1' "$(read_counter "$count_file")"
check_eq 'single attempt: never sleeps' '0' "$(count_lines "$sleep_log")"
check_eq 'single attempt: command status preserved' '9' "$rc"

# --- usage errors ----------------------------------------------------------
usage_count="$TMP_ROOT/count-usage"
rm -f "$usage_count"

usage_case() { # usage_case <retry args...>
  usage_out=$(env FAKE_COUNT="$usage_count" FAKE_STATUS=0 \
    $RETRY_SH "$RETRY" "$@" 2>&1)
  usage_rc=$?
}

usage_case
check_eq 'usage: no arguments exits 2' '2' "$usage_rc"
check_contains 'usage: no arguments prints usage' 'usage: retry.sh' "$usage_out"

usage_case abc 0 "label" -- "$FAKE"
check_eq 'usage: non-numeric attempts exits 2' '2' "$usage_rc"
check_contains 'usage: non-numeric attempts names the problem' 'attempts must be a positive integer' "$usage_out"

usage_case 0 0 "label" -- "$FAKE"
check_eq 'usage: attempts=0 exits 2' '2' "$usage_rc"
check_contains 'usage: attempts=0 names the problem' 'attempts must be at least 1' "$usage_out"

usage_case 3 nope "label" -- "$FAKE"
check_eq 'usage: non-numeric delay exits 2' '2' "$usage_rc"
check_contains 'usage: non-numeric delay names the problem' 'delay must be a non-negative integer' "$usage_out"

usage_case 3 -1 "label" -- "$FAKE"
check_eq 'usage: negative delay exits 2' '2' "$usage_rc"
check_contains 'usage: negative delay names the problem' 'delay must be a non-negative integer' "$usage_out"

usage_case 3 0 "label" "$FAKE"
check_eq 'usage: missing -- exits 2' '2' "$usage_rc"
check_contains 'usage: missing -- names the problem' 'expected -- before the command' "$usage_out"

usage_case 3 0 "label" --
check_eq 'usage: no command after -- exits 2' '2' "$usage_rc"
check_contains 'usage: no command after -- names the problem' 'no command after --' "$usage_out"

check_eq 'usage: no usage error ran the command' '0' "$(read_counter "$usage_count")"

# ---------------------------------------------------------------------------
printf '\n%d checks, %d failure(s)\n' "$checks" "$failures"
if [ "$failures" -ne 0 ]; then
  exit 1
fi
