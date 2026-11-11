#!/usr/bin/env bash
# shellcheck shell=bash
# swiftformat-lint.sh — shared `swiftformat --lint` runner for the Swift linters.
#
# SOURCE this file; do not execute it:
#
#   # shellcheck source=../../scripts/lib/swiftformat-lint.sh
#   # shellcheck disable=SC1091
#   . "${repo_root}/scripts/lib/swiftformat-lint.sh"
#
# Why it exists: a statically linked SwiftFormat build can segfault
# intermittently — exit 139, SIGSEGV — on a particular host, with no relation to
# the code being linted. The process dies before it can report a verdict, so one
# crash used to fail the whole lint job even though the changed files had never
# actually been checked. The Linux release binaries are exactly that static
# build (the dynamic build needs a newer glibc than some runners expose), which
# is the configuration that misbehaves.
#
# A crash is a TOOL failure, not a finding:
#
#   * exit 139 (SIGSEGV, i.e. death by signal 11) is retried, up to
#     SWIFTFORMAT_MAX_ATTEMPTS attempts;
#   * exit 1 (a formatting finding) and every other status are returned
#     immediately — a real finding must fail fast and is never retried;
#   * if every attempt crashes, the function says the toolchain crashed and the
#     files were never linted, then returns 139, so a dead tool can never read
#     as a clean lint or as a formatting failure.
#
# Both apps/ios/Scripts/lint-swift.sh and apps/connector.mac/Scripts/lint-swift.sh
# call this, so the two ratchets cannot drift on crash handling — use the
# function rather than an inline `swiftformat` invocation. The contract below is
# pinned by swiftformat-lint.test.sh next to this file (run by the required `ci`
# job), which drives a stub binary that exits a scripted status, so no real
# SwiftFormat installation is needed to test it.

# Attempts per run, including the first.
#
# Validated here, at source time: a malformed value used to reach the arithmetic
# comparison inside the loop, where `[ abc -ge abc ]` fails with "integer
# expression expected" and the function returned 1 — the status that means
# "formatting findings", i.e. exactly the conflation this helper exists to
# prevent. An unusable value now falls back to the default with a warning, so a
# crash is still reported as a crash and still ends as 139.
SWIFTFORMAT_MAX_ATTEMPTS_DEFAULT=3
case "${SWIFTFORMAT_MAX_ATTEMPTS:-}" in
  '')
    SWIFTFORMAT_MAX_ATTEMPTS=$SWIFTFORMAT_MAX_ATTEMPTS_DEFAULT
    ;;
  *[!0-9]*)
    printf 'lint-swift: ignoring SWIFTFORMAT_MAX_ATTEMPTS=%s (not a positive integer); using %d\n' \
      "$SWIFTFORMAT_MAX_ATTEMPTS" "$SWIFTFORMAT_MAX_ATTEMPTS_DEFAULT" >&2
    SWIFTFORMAT_MAX_ATTEMPTS=$SWIFTFORMAT_MAX_ATTEMPTS_DEFAULT
    ;;
  *)
    # `10#` so a zero-padded value ("03") is read as decimal rather than
    # tripping the test builtin's octal/invalid-integer handling.
    SWIFTFORMAT_MAX_ATTEMPTS=$((10#$SWIFTFORMAT_MAX_ATTEMPTS))
    if [ "$SWIFTFORMAT_MAX_ATTEMPTS" -lt 1 ]; then
      printf 'lint-swift: ignoring SWIFTFORMAT_MAX_ATTEMPTS=%s (must be at least 1); using %d\n' \
        "$SWIFTFORMAT_MAX_ATTEMPTS" "$SWIFTFORMAT_MAX_ATTEMPTS_DEFAULT" >&2
      SWIFTFORMAT_MAX_ATTEMPTS=$SWIFTFORMAT_MAX_ATTEMPTS_DEFAULT
    fi
    ;;
esac

# swiftformat_lint_retry_on_crash <config> <file>...
#
# Runs `swiftformat --lint --config <config> <file>...`. Returns 0 on success,
# the tool's own status for anything that is not a crash, 139 when every attempt
# crashed, and 2 when called without a config or without files — a usage error,
# deliberately distinct from both a finding (1) and a crash (139).
swiftformat_lint_retry_on_crash() {
  if [ "$#" -eq 0 ]; then
    printf '%s\n' "lint-swift: swiftformat_lint_retry_on_crash: expected a config path and at least one file" >&2
    return 2
  fi
  local config="$1"
  shift
  if [ "$#" -eq 0 ]; then
    # Nothing to lint is a caller mistake, not a clean tree: returning 0 here
    # would report success without running anything.
    printf 'lint-swift: swiftformat_lint_retry_on_crash: no files given for %s\n' "$config" >&2
    return 2
  fi
  local attempt=1
  local rc=0
  while :; do
    rc=0
    swiftformat --lint --config "$config" "$@" || rc=$?
    if [ "$rc" -eq 0 ]; then
      return 0
    fi
    if [ "$rc" -ne 139 ]; then
      # 1 = formatting findings; anything else is a deterministic tool error.
      # Neither is a crash, so neither is retried.
      return "$rc"
    fi
    if [ "$attempt" -ge "$SWIFTFORMAT_MAX_ATTEMPTS" ]; then
      printf '%s\n' \
        "lint-swift: swiftformat crashed (exit 139, SIGSEGV) on every attempt (${attempt}/${SWIFTFORMAT_MAX_ATTEMPTS})." \
        "lint-swift: the lint toolchain crashed — the files were never linted, so this is NOT a formatting finding." \
        "lint-swift: re-run the job; if it keeps crashing, investigate the installed SwiftFormat build on this host." >&2
      return 139
    fi
    attempt=$((attempt + 1))
    printf 'lint-swift: swiftformat crashed (exit 139, SIGSEGV) — retrying (%d/%d)\n' \
      "$attempt" "$SWIFTFORMAT_MAX_ATTEMPTS" >&2
  done
}
