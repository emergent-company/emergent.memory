#!/usr/bin/env bash
# Hermetic tests for swiftformat-lint.sh — no real SwiftFormat needed.
#
# The helper decides whether a red Swift linter is a *finding* (exit 1) or a
# dead tool (exit 139, SIGSEGV), and the retry is built on that decision. Every
# failure mode here is silent: retrying a finding hides real drift behind a
# flake-shaped log line, not retrying a crash fails a release for no reason, an
# exhausted retry that returned 1 would read as "you have formatting to fix",
# and a malformed attempt count used to fall into the arithmetic comparison and
# return 1 as well. Each is pinned below with a stub `swiftformat` on PATH that
# exits a scripted status.
#
# Two further contract points are pinned because they were reported against the
# first cut: a crash must never surface as 1, and calling the function with no
# files must be a usage error (2), not a green run that linted nothing.
#
#   bash scripts/lib/swiftformat-lint.test.sh
# Every case runs the helper in a subshell with its own PATH (stub first) and
# its own exported STUB_STATE, which is the point: each case gets a fresh
# invocation counter and cannot leak into the next. shellcheck reads that as a
# lost export; it is deliberate, not an accident.
# shellcheck disable=SC2030,SC2031
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
helper="$here/swiftformat-lint.sh"
if [ ! -f "$helper" ]; then
  echo "swiftformat-lint.test.sh: cannot find swiftformat-lint.sh next to this test ($helper)" >&2
  exit 1
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/swiftformat-lint.test.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

failures=0
ok() { printf 'ok   %s\n' "$1"; }
no() { printf 'FAIL %s\n' "$1"; failures=$((failures + 1)); }

# The stub: records every call (one line of arguments per call), then exits the
# next status from $STUB_STATE/statuses, falling back to $STUB_STATE/default
# once that list is exhausted. `exit 139` is what the shell reports when the
# real static binary dies of SIGSEGV, so the helper sees exactly the status it
# sees in CI.
stub_dir="$tmp/bin"
mkdir -p "$stub_dir"
cat >"$stub_dir/swiftformat" <<'STUB'
#!/usr/bin/env bash
state=${STUB_STATE:?swiftformat stub: STUB_STATE must be set}
printf '%s\n' "$*" >>"$state/calls"
n=$(wc -l <"$state/calls")
status=$(sed -n "${n}p" "$state/statuses" 2>/dev/null)
[ -n "$status" ] || status=$(cat "$state/default")
exit "$status"
STUB
chmod +x "$stub_dir/swiftformat"

# run_lint <case> <statuses> <default> [max-attempts]
#
# Runs the helper against the stub. <statuses> is newline-separated and may be
# empty; <default> applies once it runs out. When [max-attempts] is given it is
# exported, otherwise the variable is unset so the helper's own default applies.
run_lint() {
  local name="$1" statuses="$2" default="$3" attempts="${4-}"
  local state="$tmp/$name"
  mkdir -p "$state"
  : >"$state/calls"
  printf '%s' "$default" >"$state/default"
  printf '%s' "$statuses" >"$state/statuses"
  (
    export PATH="$stub_dir:$PATH"
    export STUB_STATE="$state"
    unset SWIFTFORMAT_MAX_ATTEMPTS
    if [ -n "$attempts" ]; then
      export SWIFTFORMAT_MAX_ATTEMPTS="$attempts"
    fi
    # shellcheck source=./swiftformat-lint.sh
    # shellcheck disable=SC1091
    . "$helper"
    swiftformat_lint_retry_on_crash "apps/ios/.swiftformat" apps/ios/Some.swift
  ) >"$state/out" 2>"$state/err"
  printf '%s' "$?" >"$state/rc"
}

rc_of() { cat "$tmp/$1/rc"; }
calls_of() {
  if [ -f "$tmp/$1/calls" ]; then wc -l <"$tmp/$1/calls" | tr -d ' '; else printf '0'; fi
}
err_of() { cat "$tmp/$1/err"; }

check() { # check <label> <expected> <actual>
  if [ "$2" = "$3" ]; then ok "$1"; else no "$1 (want '$2', got '$3')"; fi
}
check_contains() { # check_contains <label> <needle> <haystack>
  case "$3" in
    *"$2"*) ok "$1" ;;
    *) no "$1 (missing '$2' in: $3)" ;;
  esac
}
check_lacks() { # check_lacks <label> <needle> <haystack>
  case "$3" in
    *"$2"*) no "$1 (unexpected '$2' in: $3)" ;;
    *) ok "$1" ;;
  esac
}

forward_cfg="apps/ios/.swiftformat"
forward_file="apps/ios/Some.swift"

# ── a clean run ─────────────────────────────────────────────────────────────
run_lint clean '' 0
check "clean run exits 0" 0 "$(rc_of clean)"
check "clean run invokes swiftformat once" 1 "$(calls_of clean)"
check "the config and file are forwarded after --lint" \
  "--lint --config $forward_cfg $forward_file" "$(cat "$tmp/clean/calls")"

# ── a real finding: never retried ───────────────────────────────────────────
run_lint finding '' 1
check "a formatting finding keeps exit 1" 1 "$(rc_of finding)"
check "a formatting finding is not retried" 1 "$(calls_of finding)"
check_lacks "a formatting finding logs no retry" "retrying" "$(err_of finding)"

# ── some other tool error: never retried, status preserved ──────────────────
run_lint tool_error '' 2
check "a non-crash tool error keeps its exit 2" 2 "$(rc_of tool_error)"
check "a non-crash tool error is not retried" 1 "$(calls_of tool_error)"

# ── a crash: retried, then succeeds ─────────────────────────────────────────
run_lint crash_then_ok '139' 0
check "crash then success exits 0" 0 "$(rc_of crash_then_ok)"
check "crash then success took two attempts" 2 "$(calls_of crash_then_ok)"
check_contains "the crash is logged as a crash" \
  "swiftformat crashed (exit 139, SIGSEGV) — retrying (2/3)" "$(err_of crash_then_ok)"

# ── crash, crash, then a real finding: the finding wins ─────────────────────
run_lint crash_crash_finding $'139\n139' 1
check "a finding after two crashes keeps exit 1" 1 "$(rc_of crash_crash_finding)"
check "a finding after two crashes took three attempts" 3 "$(calls_of crash_crash_finding)"

# ── always crashing: never reported as a finding, never green ───────────────
run_lint always_crash '' 139
check "exhausted retries exit 139, not 1" 139 "$(rc_of always_crash)"
check "exhausted retries stop at three attempts" 3 "$(calls_of always_crash)"
check_contains "the exhaustion names the toolchain crash" \
  "crashed (exit 139, SIGSEGV) on every attempt (3/3)" "$(err_of always_crash)"
check_contains "the exhaustion says the files were never linted" \
  "never linted" "$(err_of always_crash)"
check_contains "the exhaustion denies being a finding" \
  "NOT a formatting finding" "$(err_of always_crash)"

# ── a malformed attempt count: falls back to the default, still 139 ─────────
run_lint bad_attempts '' 139 abc
check "a malformed attempt count still exits 139, not 1" 139 "$(rc_of bad_attempts)"
check "a malformed attempt count falls back to three attempts" 3 "$(calls_of bad_attempts)"
check_contains "the malformed attempt count is named" \
  "ignoring SWIFTFORMAT_MAX_ATTEMPTS=abc" "$(err_of bad_attempts)"
check_lacks "a malformed attempt count produces no integer error" \
  "integer expression expected" "$(err_of bad_attempts)"

run_lint zero_attempts '' 139 0
check "a zero attempt count still exits 139, not 1" 139 "$(rc_of zero_attempts)"
check "a zero attempt count falls back to three attempts" 3 "$(calls_of zero_attempts)"
check_contains "the zero attempt count is named" \
  "must be at least 1" "$(err_of zero_attempts)"

# ── a valid override is honoured ────────────────────────────────────────────
run_lint two_attempts '' 139 2
check "an explicit attempt count is honoured" 2 "$(calls_of two_attempts)"
check "an explicit attempt count still exits 139, not 1" 139 "$(rc_of two_attempts)"

# ── usage: no files ─────────────────────────────────────────────────────────
usage_state="$tmp/usage"
mkdir -p "$usage_state"
(
  export PATH="$stub_dir:$PATH"
  export STUB_STATE="$usage_state"
  unset SWIFTFORMAT_MAX_ATTEMPTS
  # shellcheck source=./swiftformat-lint.sh
  # shellcheck disable=SC1091
  . "$helper"
  swiftformat_lint_retry_on_crash "apps/ios/.swiftformat"
) >"$usage_state/out" 2>"$usage_state/err"
rc=$?
check "no files is a usage error (2), not a green run" 2 "$rc"
check "no files never invokes swiftformat" 0 "$(calls_of usage)"
check_contains "no files says so" "no files given" "$(cat "$usage_state/err")"

# ── usage: no arguments at all ──────────────────────────────────────────────
(
  export PATH="$stub_dir:$PATH"
  export STUB_STATE="$usage_state"
  unset SWIFTFORMAT_MAX_ATTEMPTS
  # shellcheck source=./swiftformat-lint.sh
  # shellcheck disable=SC1091
  . "$helper"
  swiftformat_lint_retry_on_crash
) >"$usage_state/out2" 2>"$usage_state/err2"
rc=$?
check "no arguments is a usage error (2)" 2 "$rc"
check_contains "no arguments says what is missing" "expected a config path" "$(cat "$usage_state/err2")"

echo
if [ "$failures" -ne 0 ]; then
  echo "swiftformat-lint.test.sh: $failures check(s) failed" >&2
  exit 1
fi
echo "swiftformat-lint.test.sh: all checks passed"
