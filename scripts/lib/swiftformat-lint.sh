#!/usr/bin/env bash
# shellcheck shell=bash
# swiftformat-lint.sh — shared `swiftformat --lint` runner for the Swift linters.
#
# SOURCE this file; do not execute it:
#
#   # shellcheck source=../../scripts/lib/swiftformat-lint.sh
#   . "${repo_root}/scripts/lib/swiftformat-lint.sh"
#
# Why it exists: the Linux release of SwiftFormat is a *static* Swift binary
# (see the install step in .github/workflows/ios.yml — the dynamic build needs a
# newer glibc than some self-hosted runners expose), and static-SDK Swift builds
# are known to die with SIGSEGV on particular hosts/inputs
# (swiftlang/swift#77841, realm/SwiftLint#6287). SwiftFormat 0.63.0 segfaulted
# once in ~320 identical local runs on the rig, and it failed `ios-lint` — and
# with it the required `promotion` context — on a `dev -> main` promotion PR
# whose head changed no Swift source at all (infra#1133).
#
# A crash is a TOOL failure, not a finding:
#
#   * exit 139 (SIGSEGV, i.e. death by signal 11) is retried, up to
#     SWIFTFORMAT_MAX_ATTEMPTS attempts;
#   * exit 1 (a formatting finding) and every other status are returned
#     immediately — a real finding must fail fast and is never retried;
#   * if every attempt crashes, the function reports that the toolchain crashed
#     and the files were NEVER linted, then returns 139, so a dead tool can
#     never read as a clean lint or as a formatting failure.
#
# Both apps/ios/Scripts/lint-swift.sh and apps/connector.mac/Scripts/lint-swift.sh
# call this, so the two ratchets cannot drift on crash handling — use the
# function rather than an inline `swiftformat` invocation.

# Total attempts, including the first. Overridable so the retry can be tested.
SWIFTFORMAT_MAX_ATTEMPTS="${SWIFTFORMAT_MAX_ATTEMPTS:-3}"

# swiftformat_lint_retry_on_crash <config> <file>...
#
# Runs `swiftformat --lint --config <config> <file>...`. Returns 0 on success,
# the tool's own status for anything that is not a crash, and 139 when every
# attempt crashed.
swiftformat_lint_retry_on_crash() {
  local config="$1"
  shift
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
        "lint-swift: THE TOOLCHAIN CRASHED — the changed files were NEVER linted; this is NOT a formatting failure." \
        "lint-swift: re-run the job, and if it keeps crashing investigate the static swiftformat build on this host (infra#1133)." >&2
      return 139
    fi
    attempt=$((attempt + 1))
    printf 'lint-swift: swiftformat crashed (exit 139, SIGSEGV) — retrying (%d/%d)\n' \
      "$attempt" "$SWIFTFORMAT_MAX_ATTEMPTS" >&2
  done
}
