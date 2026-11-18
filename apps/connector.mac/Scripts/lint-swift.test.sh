#!/usr/bin/env bash
# Hermetic tests for apps/connector.mac/Scripts/lint-swift.sh — no Swift toolchain
# or real linter is needed.
#
# The script used to fail OPEN: when swiftformat/swiftlint were absent it printed
# a skip and exited 0, so the lefthook `connector-mac-swift` hook reported green
# without linting anything and a count-sensitive .swiftlint.baseline regression
# could reach `dev` unseen (#925). These cases pin the fail-closed contract:
#
#   * a missing tool is a non-zero exit with Linux and macOS install guidance;
#   * the explicit local opt-out (LINT_SWIFT_SKIP=1) warns loudly and only then
#     exits 0;
#   * CI refuses the opt-out (GitHub Actions and a truthy CI alike);
#   * a malformed opt-out value is an error, not a silent skip;
#   * when the tools are present the ratchet actually runs both of them.
#
# PATH is stripped to a temp bin of symlinks, so a developer host with Homebrew
# installed still exercises the missing-tool branch and a host without it still
# exercises the lint branch through stub binaries.
#
#   bash apps/connector.mac/Scripts/lint-swift.test.sh
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/lint-swift.sh"
if [ ! -f "$script" ]; then
  echo "lint-swift.test.sh: cannot find lint-swift.sh next to this test ($script)" >&2
  exit 1
fi
repo_root=$(cd "$here/../../.." && pwd -P)
target="apps/connector.mac/MemoryConnector/Sources/AppStatus.swift"
bash_bin=$(command -v bash)

tmp=$(mktemp -d "${TMPDIR:-/tmp}/connector-lint-swift.test.XXXXXX") || exit 1
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

failures=0
ok() { printf 'ok   %s\n' "$1"; }
no() { printf 'FAIL %s\n' "$1"; failures=$((failures + 1)); }
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

# Only the commands the script itself needs: its PATH is replaced, so nothing
# from the host (swiftformat/swiftlint/brew) can leak in.
base_bin="$tmp/base-bin"
mkdir -p "$base_bin"
for cmd in git dirname basename bash; do
  cmd_path=$(command -v "$cmd" || true)
  if [ -z "$cmd_path" ]; then
    echo "lint-swift.test.sh: host is missing '$cmd', cannot run the script under a stripped PATH" >&2
    exit 1
  fi
  ln -s "$cmd_path" "$base_bin/$cmd"
done
# The script prefers `realpath -m`; without it, its cd/pwd fallback runs.
if realpath_path=$(command -v realpath); then
  ln -s "$realpath_path" "$base_bin/realpath"
fi

make_stub() { # make_stub <path>
  cat >"$1" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "${0##*/}" >>"${STUB_STATE:?}/calls"
exit 0
STUB
  chmod +x "$1"
}

tools_present="$tmp/tools-present"
mkdir -p "$tools_present"
make_stub "$tools_present/swiftformat"
make_stub "$tools_present/swiftlint"

tools_partial="$tmp/tools-partial"
mkdir -p "$tools_partial"
make_stub "$tools_partial/swiftformat"

# run_case <name> <tools-dir> [VAR=VALUE ...]
#
# Runs the script from the repo root with a PATH of only <tools-dir> plus the
# host-command symlinks. CI/LINT_SWIFT_SKIP start unset unless the case sets
# them, so the host's environment cannot decide the outcome. STUB_STATE is
# pointed at the case directory, which is how the stubs record their calls.
run_case() {
  local name="$1" tools_dir="$2"
  shift 2
  local state="$tmp/$name"
  mkdir -p "$state"
  (
    cd "$repo_root" || exit 99
    export PATH="$tools_dir:$base_bin"
    unset CI GITHUB_ACTIONS LINT_SWIFT_SKIP STUB_STATE
    export STUB_STATE="$state"
    local assignment
    for assignment in "$@"; do
      export "$assignment"
    done
    "$bash_bin" "$script" "$target"
  ) >"$state/out" 2>"$state/err"
  printf '%s' "$?" >"$state/rc"
}

rc_of() { cat "$tmp/$1/rc"; }
err_of() { cat "$tmp/$1/err"; }
out_of() { cat "$tmp/$1/out"; }
calls_of() {
  if [ -f "$tmp/$1/calls" ]; then cat "$tmp/$1/calls"; else printf ''; fi
}
call_count_of() {
  if [ -f "$tmp/$1/calls" ]; then wc -l <"$tmp/$1/calls" | tr -d ' '; else printf '0'; fi
}

# ── missing tools: fail closed, never a silent skip ─────────────────────────
run_case missing_tools "$tmp/no-tools"
check "missing both linters exits non-zero" 1 "$(rc_of missing_tools)"
check_contains "the missing tools are named" \
  "missing Mac connector lint tool(s): swiftformat swiftlint" "$(err_of missing_tools)"
check_contains "the script says it is not a clean result" \
  "NOT a clean result" "$(err_of missing_tools)"
check_contains "the failure says it fails closed" "failing closed" "$(err_of missing_tools)"
check_contains "macOS install guidance is given" \
  "brew install swiftformat swiftlint" "$(err_of missing_tools)"
check_contains "the Linux SwiftFormat asset is named" \
  "swiftformat_linux.zip" "$(err_of missing_tools)"
check_contains "the Linux SwiftLint asset is named" \
  "swiftlint_linux_amd64.zip" "$(err_of missing_tools)"
check_contains "the pinned-version location is named" \
  "SWIFTFORMAT_VERSION" "$(err_of missing_tools)"
check_lacks "a missing-tool run never claims success/skips silently" \
  "SKIPPING" "$(err_of missing_tools) $(out_of missing_tools)"
check "nothing was linted" 0 "$(call_count_of missing_tools)"

# ── only one tool missing: still fail closed ────────────────────────────────
run_case missing_swiftlint "$tools_partial"
check "a single missing linter exits non-zero" 1 "$(rc_of missing_swiftlint)"
check_contains "only the absent linter is named" \
  "missing Mac connector lint tool(s): swiftlint" "$(err_of missing_swiftlint)"
check_lacks "the present linter is not claimed missing" \
  "tool(s): swiftformat" "$(err_of missing_swiftlint)"
check_lacks "no lint run happens" "swiftformat --lint" "$(out_of missing_swiftlint)"

# ── explicit local opt-out: warn loudly, then exit 0 ────────────────────────
run_case skip_local "$tmp/no-tools" LINT_SWIFT_SKIP=1
check "LINT_SWIFT_SKIP=1 exits 0 on a local host" 0 "$(rc_of skip_local)"
check_contains "the skip is a loud warning" \
  "WARNING: LINT_SWIFT_SKIP=1 — SKIPPING" "$(err_of skip_local)"
check_contains "the warning says nothing was linted" \
  "nothing was linted" "$(err_of skip_local)"
check_contains "the skip still prints install guidance" \
  "swiftformat_linux.zip" "$(err_of skip_local)"

# ── CI refuses the opt-out ──────────────────────────────────────────────────
run_case skip_github_actions "$tmp/no-tools" LINT_SWIFT_SKIP=1 GITHUB_ACTIONS=true
check "the opt-out is refused under GitHub Actions" 1 "$(rc_of skip_github_actions)"
check_contains "the refusal names CI" \
  "refused in CI" "$(err_of skip_github_actions)"
check_lacks "CI never sees the skip warning" "SKIPPING" "$(err_of skip_github_actions)"

run_case skip_ci_true "$tmp/no-tools" LINT_SWIFT_SKIP=1 CI=true
check "the opt-out is refused when CI=true" 1 "$(rc_of skip_ci_true)"
check_contains "the CI=true refusal names CI" \
  "refused in CI" "$(err_of skip_ci_true)"

# A local shell exporting CI=false must not count as CI.
run_case skip_ci_false "$tmp/no-tools" LINT_SWIFT_SKIP=1 CI=false
check "CI=false does not refuse the local opt-out" 0 "$(rc_of skip_ci_false)"

# ── a malformed opt-out is an error, not a skip ─────────────────────────────
run_case skip_bad_value "$tmp/no-tools" LINT_SWIFT_SKIP=0
check "LINT_SWIFT_SKIP=0 exits non-zero" 1 "$(rc_of skip_bad_value)"
check_contains "the bad value is named" \
  "not a recognised opt-out" "$(err_of skip_bad_value)"
check_lacks "a bad value never skips" "SKIPPING" "$(err_of skip_bad_value)"

# ── tools present: the ratchet really runs both linters ─────────────────────
run_case lint_runs "$tools_present"
check "a complete toolchain lints the file" 0 "$(rc_of lint_runs)"
check "both linters are invoked" 2 "$(call_count_of lint_runs)"
check_contains "swiftformat ran" "swiftformat" "$(calls_of lint_runs)"
check_contains "swiftlint ran" "swiftlint" "$(calls_of lint_runs)"
check_lacks "the toolchain is not reported missing" \
  "missing Mac connector lint tool(s)" "$(err_of lint_runs)"
check_lacks "no skip warning when the tools are present" \
  "WARNING" "$(err_of lint_runs)"

# The opt-out must not disable linting when the tools are installed.
run_case skip_with_tools "$tools_present" LINT_SWIFT_SKIP=1
check "the opt-out does not skip a complete toolchain" 0 "$(rc_of skip_with_tools)"
check "both linters still run with the opt-out set" 2 "$(call_count_of skip_with_tools)"
check_lacks "the opt-out never prints the skip warning when tools exist" \
  "SKIPPING" "$(err_of skip_with_tools)"

echo
if [ "$failures" -ne 0 ]; then
  echo "lint-swift.test.sh: $failures check(s) failed" >&2
  exit 1
fi
echo "lint-swift.test.sh: all checks passed"
