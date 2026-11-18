#!/usr/bin/env bash
# Hermetic regression suite for reconcile-release-tag.py (#1047).
#
# WHY: the release tag is created by `auto-tag.yml` on a `push` to `main` that
# touches `VERSION`. A release PR merged by `app/github-actions` raises that push
# from `GITHUB_TOKEN`, for which GitHub starts **no workflow runs** -- so on
# v0.104.0 the tag simply never appeared and the nine-workflow fan-out never
# started. `reconcile-release-tag.py` is the decision table behind the
# dispatch/schedule triggers that are exempt from that suppression. Every branch
# of that table is a comparison whose failure is silent in the wrong direction:
#
#   * dispatching while a run is already queued stacks a second release job;
#   * re-dispatching after a run *failed* turns one broken release into an
#     endless loop with a green schedule -- the same "observed by nobody" shape
#     the issue is about;
#   * reading a 403/500 as "tag missing" dispatches against a tag that exists;
#   * honouring `[skip release]` only on the tip ignores a deliberate no-tag
#     bump (or, the other way round, tags a snapshot push);
#   * a run for a *previous* bump must never be read as evidence about this one.
#
# The suite stubs `gh` on PATH and serves fixtures, so it needs no credentials
# and no network. It is wired into `scripts/preflight/all.sh`, which the required
# `ci` job runs, so a pull request exercises it without a workflow-file change.
#
# Usage:
#   .github/scripts/reconcile-release-tag.test.sh
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/reconcile-release-tag.py"
REPO=emergent-company/emergent.memory

if [[ ! -f $script ]]; then
  printf 'FAIL [reconcile-release-tag]: script not found at %s\n' "$script" >&2
  exit 1
fi
for cmd in python3 jq; do
  command -v "$cmd" >/dev/null 2>&1 || {
    printf 'FAIL [reconcile-release-tag]: %s is required\n' "$cmd" >&2
    exit 2
  }
done

checks=0
fail=0
ok() { checks=$((checks + 1)); printf 'ok [reconcile-release-tag]: %s\n' "$1"; }
bad() { checks=$((checks + 1)); printf 'FAIL [reconcile-release-tag]: %s\n' "$1" >&2; fail=1; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
stub_bin="$tmp/bin"
fix="$tmp/fixtures"
mkdir -p "$stub_bin" "$fix"

TIP=1111111111111111111111111111111111111111
BUMP=2222222222222222222222222222222222222222
OLDER=3333333333333333333333333333333333333333
output=""

# --- the fake `gh` -----------------------------------------------------------
# Serves exactly the reads the script makes (VERSION contents, a tag ref,
# commits, workflow runs) plus the dispatch, from files under $FIX. `--jq` is
# applied with the real jq, so the script's own expressions are exercised.
cat >"$stub_bin/gh" <<'STUB'
#!/usr/bin/env python3
import base64
import json
import os
import pathlib
import subprocess
import sys

fix = pathlib.Path(os.environ["FIX"])
repo = os.environ["FIX_REPO"]
args = sys.argv[1:]
with open(fix / "calls.log", "a", encoding="utf-8") as fh:
    fh.write(" ".join(args) + "\n")


def emit(payload):
    text = payload if isinstance(payload, str) else json.dumps(payload)
    if "--jq" in args:
        expr = args[args.index("--jq") + 1]
        proc = subprocess.run(["jq", "-r", expr], input=text, text=True,
                              capture_output=True)
        sys.stdout.write(proc.stdout)
        sys.stderr.write(proc.stderr)
        sys.exit(proc.returncode)
    sys.stdout.write(text + "\n")
    sys.exit(0)


def not_found():
    sys.stderr.write("gh: Not Found (HTTP 404)\n")
    sys.exit(1)


def server_error():
    sys.stderr.write("gh: Server Error (HTTP 500)\n")
    sys.exit(1)


if args[:2] == ["workflow", "run"]:
    flag = fix / "dispatch_exit"
    sys.exit(int(flag.read_text().strip()) if flag.exists() else 0)

if args[:1] != ["api"]:
    sys.stderr.write("unexpected gh invocation: %s\n" % " ".join(args))
    sys.exit(2)

path = args[1]

if path.startswith("repos/%s/contents/VERSION" % repo):
    ref = path.split("?ref=", 1)[1] if "?ref=" in path else ""
    candidate = fix / ("version.%s" % ref)
    if not candidate.exists():
        candidate = fix / "version"
    if not candidate.exists():
        not_found()
    raw = candidate.read_text(encoding="utf-8")
    emit({"encoding": "base64",
          "content": base64.b64encode(raw.encode()).decode() + "\n"})

if path.startswith("repos/%s/git/ref/tags/" % repo):
    if (fix / "tag_error").exists():
        server_error()
    if (fix / "tag_exists").exists():
        emit({"ref": "refs/tags/served"})
    not_found()

if path.startswith("repos/%s/commits?" % repo):
    candidate = fix / "bump.json"
    if not candidate.exists():
        not_found()
    emit(json.loads(candidate.read_text(encoding="utf-8")))

if path.startswith("repos/%s/commits/" % repo):
    ref = path.rsplit("/", 1)[1]
    candidate = fix / ("commit.%s.json" % ref)
    if candidate.exists():
        emit(json.loads(candidate.read_text(encoding="utf-8")))
    if (fix / "tip.json").exists() and len(ref) != 40:
        emit(json.loads((fix / "tip.json").read_text(encoding="utf-8")))
    not_found()

if "/actions/workflows/" in path:
    candidate = fix / "runs.json"
    if not candidate.exists():
        emit({"workflow_runs": []})
    emit(json.loads(candidate.read_text(encoding="utf-8")))

sys.stderr.write("unexpected gh api path: %s\n" % path)
sys.exit(2)
STUB
chmod +x "$stub_bin/gh"

# --- fixtures ----------------------------------------------------------------
fresh() {
  rm -rf "$fix"
  mkdir -p "$fix"
  : >"$fix/calls.log"
  printf '0.104.0' >"$fix/version"
  jq -nc --arg sha "$TIP" --arg m "chore: release v0.104.0" \
    '{sha: $sha, commit: {message: $m}}' >"$fix/tip.json"
  jq -nc --arg sha "$BUMP" --arg m "chore: release v0.104.0" \
    '[{sha: $sha, commit: {message: $m}}]' >"$fix/bump.json"
}

set_tip_message() {
  jq -nc --arg sha "$TIP" --arg m "$1" '{sha: $sha, commit: {message: $m}}' >"$fix/tip.json"
}
set_bump_message() {
  jq -nc --arg sha "$BUMP" --arg m "$1" '[{sha: $sha, commit: {message: $m}}]' >"$fix/bump.json"
}
set_runs() { printf '%s' "$1" >"$fix/runs.json"; }

run_case() { # expected_exit [args...]
  local want=$1 got
  shift
  set +e
  FIX="$fix" FIX_REPO="$REPO" PATH="$stub_bin:$PATH" \
    python3 "$script" --repo "$REPO" "$@" >"$tmp/out" 2>"$tmp/err"
  got=$?
  set -e
  output=$(cat "$tmp/out" "$tmp/err")
  if [[ $got -eq $want ]]; then
    ok "exit $want: $*"
  else
    bad "expected exit $want, got $got: $* (output: $output)"
  fi
}
expect_output() { # substring
  if [[ $output == *"$1"* ]]; then
    ok "output mentions: $1"
  else
    bad "output does not mention '$1': $output"
  fi
}
expect_dispatches() { # count
  local got
  got=$(grep -c '^workflow run' "$fix/calls.log" || true)
  if [[ ${got:-0} -eq $1 ]]; then
    ok "$1 dispatch(es)"
  else
    bad "expected $1 dispatch(es), got ${got:-0}"
  fi
}

# 1. Consistent: the tag exists. Nothing may be dispatched, whatever else is on
#    disk -- this is the steady state of every scheduled tick.
fresh
: >"$fix/tag_exists"
run_case 0
expect_output "tag v0.104.0 exists"
expect_dispatches 0

# 2. The hole: VERSION bumped, tag missing, nothing ever ran -> dispatch. This
#    is the case the schedule exists for.
fresh
run_case 0
expect_output "Dispatched auto-tag.yml on main for v0.104.0"
expect_dispatches 1

# 3. A run is already queued/in progress: wait, do not stack a second release
#    job on top of it.
fresh
set_runs "$(jq -nc --arg sha "$BUMP" '{workflow_runs: [{id: 7, head_sha: $sha, status: "in_progress", conclusion: null}]}')"
run_case 0
expect_output "run 7 for 222222222 is in_progress"
expect_dispatches 0

# 4. A run finished successfully and the tag is still missing: that is a real
#    failure, not something to retry every ten minutes.
fresh
set_runs "$(jq -nc --arg sha "$BUMP" '{workflow_runs: [{id: 8, head_sha: $sha, status: "completed", conclusion: "success"}]}')"
run_case 1
expect_output "already finished (success)"
expect_output "gh run rerun 8"
expect_dispatches 0

# 5. Same for a failed run.
fresh
set_runs "$(jq -nc --arg sha "$BUMP" '{workflow_runs: [{id: 9, head_sha: $sha, status: "completed", conclusion: "failure"}]}')"
run_case 1
expect_output "already finished (failure)"
expect_dispatches 0

# 6. `[skip release]` on the commit that bumped VERSION: deliberately untagged.
fresh
set_bump_message "chore: bump VERSION [skip release]"
run_case 0
expect_output "deliberately untagged"
expect_dispatches 0

# 7. The marker on the branch tip (what the tag would point at) also skips.
fresh
set_tip_message "re-land v0.104.0 after the snapshot push [skip release]"
run_case 0
expect_output "deliberately untagged"
expect_dispatches 0

# 8. A completed run for a *previous* bump is not evidence about this one: the
#    tag is missing and this bump never ran, so it must still be dispatched.
fresh
set_runs "$(jq -nc --arg sha "$OLDER" '{workflow_runs: [{id: 10, head_sha: $sha, status: "completed", conclusion: "failure"}]}')"
run_case 0
expect_output "Dispatched auto-tag.yml on main for v0.104.0"
expect_dispatches 1

# 9. A tag lookup that is not a 404 (permissions, outage) must fail closed --
#    never read as "missing" and dispatch against a tag that may exist.
fresh
: >"$fix/tag_error"
run_case 2
expect_output "Refusing to dispatch"
expect_dispatches 0

# 10. A failed dispatch is loud and names the manual recovery. A 422 here is the
#     pre-#1047 state: the workflow declares no `workflow_dispatch`.
fresh
printf '1' >"$fix/dispatch_exit"
run_case 1
expect_output "could not dispatch auto-tag.yml"
expect_output "git tag -a v0.104.0 222222222"
expect_dispatches 1

# 11. `--dry-run` reports the decision without dispatching.
fresh
run_case 0 --dry-run
expect_output "WOULD DISPATCH"
expect_dispatches 0

# 12. An empty VERSION is not a release: fail, do not tag "v".
fresh
printf '  \n' >"$fix/version"
run_case 2
expect_output "VERSION is empty"
expect_dispatches 0

# 13. An unreadable bump commit (404 from the API) is unreadable state, not
#     "dispatch": the decision table has no row for it.
fresh
rm -f "$fix/bump.json"
run_case 2
expect_output "Refusing to dispatch"
expect_dispatches 0

# 14. No repository to read means no safe answer.
fresh
set +e
FIX="$fix" FIX_REPO="$REPO" PATH="$stub_bin:$PATH" REPO= GITHUB_REPOSITORY= \
  python3 "$script" >"$tmp/out" 2>"$tmp/err"
got=$?
set -e
output=$(cat "$tmp/out" "$tmp/err")
if [[ $got -eq 2 ]]; then
  ok "exit 2 without a repository"
else
  bad "expected exit 2 without a repository, got $got: $output"
fi
expect_dispatches 0

if [[ $fail -ne 0 ]]; then
  printf 'FAIL [reconcile-release-tag]: %d checks, at least one failed\n' "$checks" >&2
  exit 1
fi
printf 'PASS [reconcile-release-tag]: %d checks\n' "$checks"
