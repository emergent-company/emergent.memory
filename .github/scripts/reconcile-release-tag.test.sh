#!/usr/bin/env bash
# Hermetic suite for .github/scripts/reconcile-release-tag.py (#1047).
#
# WHY: the push that a review-gate merge produces comes from GITHUB_TOKEN and
# starts no workflow runs, so `auto-tag.yml` never ran for v0.104.0 and the tag
# had to be pushed by hand. The reconcile script is the observer that cannot be
# suppressed (schedule/dispatch), and its decision branches are the difference
# between self-healing and a release that silently never happens — or between
# one dispatch and a dispatch on every schedule tick forever. Each branch is
# driven here through a fake `gh`, asserting the exit status, the decision, and
# whether `workflow run auto-tag.yml` was called.
#
# It also pins the wiring this fix depends on: auto-tag must stay dispatchable,
# the watchdog workflow must exist on a schedule and reference this script, and
# the review gate must call the post-merge dispatch helper. Removing any of them
# would put the v0.104.0 failure back while this suite stayed green.
#
# Usage:
#   .github/scripts/reconcile-release-tag.test.sh
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../.." && pwd)
script="$here/reconcile-release-tag.py"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
fixtures="$tmp/fixtures"
mkdir -p "$fixtures" "$tmp/bin"
export FIXTURES="$fixtures"
export REPO="o/r"

checks=0
failures=0
ok() { checks=$((checks + 1)); printf 'ok   %s\n' "$1"; }
bad() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'FAIL %s\n' "$1"; }

if [[ ! -f "$script" ]]; then
  echo "reconcile-release-tag: missing $script" >&2
  exit 1
fi

# A fake `gh`: API reads come from fixtures, `workflow run` is recorded. Each
# API path must be recognised explicitly — a new call in the script fails here
# instead of silently reading the wrong fixture.
cat >"$tmp/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
printf '%s\n' "$*" >>"$FIXTURES/calls.log"
if [[ "${1:-}" == "api" ]]; then
  path=""
  for arg in "$@"; do
    case "$arg" in
      *"/contents/VERSION"*) path="version" ;;
      *"/git/ref/tags/"*) path="tag" ;;
      *"/commits?path=VERSION"*) path="commits" ;;
      *"/actions/workflows/"*) path="runs" ;;
    esac
  done
  case "$path" in
    version) cat "$FIXTURES/version" ;;
    tag)
      if [[ -f "$FIXTURES/tag-exists" ]]; then
        printf '{"ref":"refs/tags/x"}\n'
      else
        printf 'gh: Not Found (HTTP 404)\n' >&2
        exit 1
      fi
      ;;
    commits) cat "$FIXTURES/bump.json" ;;
    runs) cat "$FIXTURES/runs.json" ;;
    *)
      printf 'fake gh: unexpected api call: %s\n' "$*" >&2
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

# reset_fixtures <version> <bump-message> [bump-date]
reset_fixtures() {
  rm -f "$fixtures"/calls.log "$fixtures"/dispatch.log "$fixtures"/dispatch-rc \
    "$fixtures"/tag-exists
  printf '%s\n' "$1" >"$fixtures/version"
  cat >"$fixtures/bump.json" <<JSON
[{"sha":"595b4c16e91aaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "commit":{"message":"$2","committer":{"date":"${3:-2026-11-18T18:49:00Z}"}}}]
JSON
  printf '{"workflow_runs":[]}\n' >"$fixtures/runs.json"
}

set_runs() { printf '{"workflow_runs":%s}\n' "$1" >"$fixtures/runs.json"; }

dispatched() { [[ -s "$fixtures/dispatch.log" ]]; }

run_script() {
  local rc
  python3 "$script" --repo o/r "$@" >"$tmp/out" 2>&1
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

reset_fixtures 0.104.0 "chore: release v0.104.0"
touch "$fixtures/tag-exists"
expect "tag exists: consistent, no dispatch" 0 no

reset_fixtures 0.104.0 "chore: release v0.104.0 [skip release]"
expect "[skip release] bump: deliberately untagged, no dispatch" 0 no

reset_fixtures 0.104.0 "chore: release v0.104.0"
expect "no run for the bump: dispatch the heal" 0 yes

reset_fixtures 0.104.0 "chore: release v0.104.0"
set_runs '[{"path":".github/workflows/auto-tag.yml","status":"queued","conclusion":null,
            "created_at":"2026-11-18T18:50:00Z","html_url":"https://example/run/1"},
           {"path":".github/workflows/auto-tag.yml","status":"in_progress","conclusion":null,
            "created_at":"2026-11-18T18:50:30Z","html_url":"https://example/run/2"}]'
expect "run already in flight: wait, no duplicate dispatch" 0 no

reset_fixtures 0.104.0 "chore: release v0.104.0"
set_runs '[{"path":".github/workflows/auto-tag.yml","status":"completed","conclusion":"failure",
            "created_at":"2026-11-18T18:50:00Z","html_url":"https://example/run/1"}]'
expect "failed run: drift reported, no dispatch loop" 1 no

reset_fixtures 0.104.0 "chore: release v0.104.0"
set_runs '[{"path":".github/workflows/auto-tag.yml","status":"completed","conclusion":"success",
            "created_at":"2026-11-18T18:50:00Z","html_url":"https://example/run/1"}]'
expect "successful run but no tag: half-state reported, no dispatch" 1 no

reset_fixtures 0.104.0 "chore: release v0.104.0"
set_runs '[{"path":".github/workflows/auto-tag.yml","status":"completed","conclusion":"failure",
            "created_at":"2026-10-01T00:00:00Z","html_url":"https://example/old"}]'
expect "run from before the bump is ignored: dispatch" 0 yes

reset_fixtures 0.104.0 "chore: release v0.104.0"
set_runs '[{"path":".github/workflows/ci.yml","status":"completed","conclusion":"failure",
            "created_at":"2026-11-18T18:50:00Z","html_url":"https://example/ci"}]'
expect "non-auto-tag runs are ignored: dispatch" 0 yes

reset_fixtures 0.104.0 "chore: release v0.104.0"
set_runs '[{"path":".github/workflows/auto-tag.yml","status":"completed","conclusion":"failure",
            "html_url":"https://example/no-date"}]'
expect "run without a timestamp is not hidden: drift reported" 1 no

reset_fixtures 0.104.0 "chore: release v0.104.0"
expect "dry run: reports, no dispatch" 0 no --dry-run

reset_fixtures 0.104 "chore: release v0.104"
expect "malformed VERSION: fail closed, no dispatch" 1 no

reset_fixtures 0.104.0 "chore: release v0.104.0"
printf '1\n' >"$fixtures/dispatch-rc"
expect "dispatch rejected by the API: fail loudly" 1 yes

# --- wiring this fix depends on ---------------------------------------------

auto_tag="$root/.github/workflows/auto-tag.yml"
watchdog="$root/.github/workflows/release-tag-watchdog.yml"
gate="$root/.github/workflows/github-review-gate.yml"

if grep -qE '^  workflow_dispatch:[[:space:]]*$' "$auto_tag"; then
  ok "auto-tag.yml is dispatchable (the documented recovery is a dispatch)"
else
  bad "auto-tag.yml has no workflow_dispatch: trigger"
fi

if grep -q 'skip release' "$auto_tag" \
  && grep -q 'git log -1 --pretty=%B -- VERSION' "$auto_tag"; then
  ok "auto-tag.yml honours [skip release] on the dispatched path too"
else
  bad "auto-tag.yml does not check the VERSION commit for [skip release]"
fi

if [[ -f "$watchdog" ]]; then
  ok "release-tag-watchdog.yml exists"
else
  bad "release-tag-watchdog.yml is missing"
fi
if grep -qE '^  schedule:' "$watchdog" 2>/dev/null \
  && grep -qE '^  workflow_dispatch:' "$watchdog" 2>/dev/null; then
  ok "watchdog runs on a schedule and on demand (both are GITHUB_TOKEN exceptions)"
else
  bad "watchdog is missing its schedule/workflow_dispatch triggers"
fi
if grep -q 'reconcile-release-tag.py' "$watchdog" 2>/dev/null; then
  ok "watchdog runs the reconcile script"
else
  bad "watchdog does not reference reconcile-release-tag.py"
fi

if grep -q 'dispatch-release-tag.sh' "$gate"; then
  ok "review gate dispatches auto-tag after the merge it arms"
else
  bad "review gate does not call dispatch-release-tag.sh"
fi

if ((failures)); then
  echo "reconcile-release-tag: FAILED ($failures/$checks)" >&2
  exit 1
fi
printf 'reconcile-release-tag: passed (%d checks)\n' "$checks"
