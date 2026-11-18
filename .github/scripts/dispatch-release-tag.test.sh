#!/usr/bin/env bash
# Hermetic suite for .github/scripts/dispatch-release-tag.sh (#1047).
#
# WHY: the review gate arms auto-merge, GitHub merges as the Actions app, and
# the push to `main` starts no workflow runs — so `auto-tag.yml` never fires and
# v0.104.0 had to be tagged by hand. The helper is the fast path out of that: it
# waits a bounded time for the merge it armed and then dispatches `auto-tag.yml`
# on `main`.
#
# The branches below are the ones that decide whether the release path works:
# a `dev` merge must never dispatch, a merge that never lands must leave the
# scheduled watchdog to it instead of failing the gate, a failed dispatch must
# be loud, and a missing/bad argument must be a usage error rather than a
# silent no-op.
#
# Usage:
#   .github/scripts/dispatch-release-tag.test.sh
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../.." && pwd)
script="$here/dispatch-release-tag.sh"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fixtures="$tmp/fixtures"
mkdir -p "$fixtures" "$tmp/bin"
export FIXTURES="$fixtures"

checks=0
failures=0
ok() { checks=$((checks + 1)); printf 'ok   %s\n' "$1"; }
bad() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'FAIL %s\n' "$1"; }

if [[ ! -f "$script" ]]; then
  echo "dispatch-release-tag: missing $script" >&2
  exit 1
fi

# Fake `gh`: PR base/state come from fixtures (states are consumed one per poll),
# and `workflow run` is recorded.
cat >"$tmp/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
if [[ "${1:-}" == "pr" && "${2:-}" == "view" ]]; then
  case "$*" in
    *"--json baseRefName"*) cat "$FIXTURES/base" ;;
    *"--json state"*)
      seq=1
      [[ -f "$FIXTURES/state.seq" ]] && seq=$(cat "$FIXTURES/state.seq")
      line=$(sed -n "${seq}p" "$FIXTURES/states")
      [[ -n "$line" ]] || line="OPEN"
      echo $((seq + 1)) >"$FIXTURES/state.seq"
      printf '%s\n' "$line"
      ;;
    *)
      printf 'fake gh: unexpected pr view: %s\n' "$*" >&2
      exit 1
      ;;
  esac
  exit 0
fi
if [[ "${1:-}" == "workflow" && "${2:-}" == "run" ]]; then
  printf '%s\n' "$*" >>"$FIXTURES/dispatch.log"
  if [[ -f "$FIXTURES/dispatch-rc" ]]; then
    exit "$(cat "$FIXTURES/dispatch-rc")"
  fi
  exit 0
fi
printf 'fake gh: unexpected invocation: %s\n' "$*" >&2
exit 1
STUB
chmod +x "$tmp/bin/gh"
export PATH="$tmp/bin:$PATH"

# setup <base> <states...>
setup() {
  rm -f "$fixtures"/dispatch.log "$fixtures"/dispatch-rc "$fixtures"/state.seq
  printf '%s\n' "$1" >"$fixtures/base"
  shift
  printf '%s\n' "$@" >"$fixtures/states"
}

dispatched() { [[ -s "$fixtures/dispatch.log" ]]; }

dispatch_count() {
  if [[ -f "$fixtures/dispatch.log" ]]; then
    wc -l <"$fixtures/dispatch.log" | tr -d ' '
  else
    echo 0
  fi
}

run_script() {
  local rc
  env REPO=o/r WAIT_SECONDS="${WAIT_SECONDS:-5}" WAIT_INTERVAL=0 \
    "$script" "$@" >"$tmp/out" 2>&1
  rc=$?
  echo "$rc"
}

expect() { # expect <label> <expected-rc> <expect-dispatch yes|no> [script args...]
  local label="$1" want_rc="$2" want_dispatch="$3"
  shift 3
  local rc
  rc=$(run_script "$@")
  if [[ "$rc" != "$want_rc" ]]; then
    bad "$label: expected rc=$want_rc, got $rc ($(tail -2 "$tmp/out" | tr '\n' ' '))"
    return
  fi
  if [[ "$want_dispatch" == "yes" ]] && ! dispatched; then
    bad "$label: expected a dispatch of auto-tag.yml, none happened"
    return
  fi
  if [[ "$want_dispatch" == "no" ]] && dispatched; then
    bad "$label: expected no dispatch, got: $(cat "$fixtures/dispatch.log")"
    return
  fi
  if [[ "$want_dispatch" == "yes" ]] \
    && ! grep -qx 'workflow run auto-tag.yml --repo o/r --ref main' "$fixtures/dispatch.log"; then
    bad "$label: dispatched the wrong command: $(cat "$fixtures/dispatch.log")"
    return
  fi
  ok "$label"
}

# --- decision branches -------------------------------------------------------

setup dev OPEN MERGED
expect "dev merge: never dispatches" 0 no 42

setup main MERGED
expect "merge lands within the wait: dispatch auto-tag on main" 0 yes 42

setup main OPEN OPEN MERGED
expect "merge lands after a few polls: dispatch" 0 yes 42
if [[ "$(dispatch_count)" == "1" ]]; then
  ok "merged PR dispatches exactly once"
else
  bad "merged PR dispatched $(dispatch_count) times"
fi

setup main OPEN OPEN OPEN
WAIT_SECONDS=1 expect "merge outlives the wait: leave it to the watchdog" 0 no 42

setup main CLOSED
expect "closed without merging: nothing to tag" 0 no 42

setup main MERGED
printf '1\n' >"$fixtures/dispatch-rc"
expect "dispatch rejected by the API: fail loudly" 1 yes 42

# --- usage -------------------------------------------------------------------

setup main MERGED
rc=$(run_script)
if [[ "$rc" == "2" ]]; then
  ok "missing PR argument is a usage error"
else
  bad "missing PR argument: expected rc=2, got $rc"
fi

rc=$(env -u REPO -u GITHUB_REPOSITORY WAIT_SECONDS=0 WAIT_INTERVAL=0 \
  "$script" 42 >"$tmp/out" 2>&1
  echo $?)
if [[ "$rc" == "2" ]]; then
  ok "missing REPO is a usage error"
else
  bad "missing REPO: expected rc=2, got $rc"
fi

# --- wiring this fix depends on ---------------------------------------------

gate="$root/.github/workflows/github-review-gate.yml"
if grep -q 'dispatch-release-tag.sh' "$gate"; then
  ok "review gate calls the post-merge dispatch helper"
else
  bad "review gate does not call dispatch-release-tag.sh"
fi
if grep -qE '^\s+uses: actions/checkout@v4' "$gate"; then
  ok "review gate checks the repository out before running repo scripts"
else
  bad "review gate runs a repo script without a checkout step"
fi

if ((failures)); then
  echo "dispatch-release-tag: FAILED ($failures/$checks)" >&2
  exit 1
fi
printf 'dispatch-release-tag: passed (%d checks)\n' "$checks"
