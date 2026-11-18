#!/usr/bin/env bash
# Hermetic regression suite for dispatch-release-tag.sh (#1047).
#
# WHY: `github-review-gate.yml` arms auto-merge and GitHub merges the PR as
# `app/github-actions`. The resulting push to `main` is raised from
# `GITHUB_TOKEN`, for which GitHub starts **no workflow runs**, so
# `auto-tag.yml`'s `on: push: paths: ['VERSION']` trigger never fires and the
# release fan-out never starts. The gate therefore dispatches `auto-tag.yml`
# itself (`workflow_dispatch` is exempt from the suppression) once the merge it
# armed lands.
#
# The helper runs *after* the PR is merged, so every mistake is unrecoverable by
# a re-run of the gate, and each branch is silent in the wrong direction:
#
#   * dispatching for a PR that did not touch `VERSION` starts a release job for
#     the current version -- a tag that may already exist, or a rebuild;
#   * dispatching for a PR merged into a non-release branch would tag that
#     branch's VERSION;
#   * failing the gate because the merge has not landed yet reports a problem
#     with something that has not happened (the schedule covers it);
#   * reading a merge commit that has no parents, or a VERSION that cannot be
#     read, as "no change" silently drops the release.
#
# The suite stubs `gh` on PATH and serves fixtures: no credentials, no network.
# It is wired into `scripts/preflight/all.sh`, which the required `ci` job runs,
# so a pull request exercises it without a workflow-file change.
#
# Usage:
#   .github/scripts/dispatch-release-tag.test.sh
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/dispatch-release-tag.sh"
REPO=emergent-company/emergent.memory
PR=1047
MERGE=4444444444444444444444444444444444444444
PARENT=5555555555555555555555555555555555555555

if [[ ! -f $script ]]; then
  printf 'FAIL [dispatch-release-tag]: script not found at %s\n' "$script" >&2
  exit 1
fi
for cmd in python3 jq base64; do
  command -v "$cmd" >/dev/null 2>&1 || {
    printf 'FAIL [dispatch-release-tag]: %s is required\n' "$cmd" >&2
    exit 2
  }
done

checks=0
fail=0
ok() { checks=$((checks + 1)); printf 'ok [dispatch-release-tag]: %s\n' "$1"; }
bad() { checks=$((checks + 1)); printf 'FAIL [dispatch-release-tag]: %s\n' "$1" >&2; fail=1; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
stub_bin="$tmp/bin"
fix="$tmp/fixtures"
mkdir -p "$stub_bin" "$fix"
output=""

# --- the fake `gh` -----------------------------------------------------------
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


if args[:2] == ["workflow", "run"]:
    flag = fix / "dispatch_exit"
    sys.exit(int(flag.read_text().strip()) if flag.exists() else 0)

if args[:2] == ["pr", "view"]:
    payload = json.loads((fix / "pr.json").read_text(encoding="utf-8"))
    keep = None
    for i, arg in enumerate(args):
        if arg == "--json":
            keep = args[i + 1].split(",")
    if keep is not None:
        payload = {k: payload.get(k) for k in keep}
    emit(payload)

if args[:1] != ["api"]:
    sys.stderr.write("unexpected gh invocation: %s\n" % " ".join(args))
    sys.exit(2)

path = args[1]

if path.startswith("repos/%s/contents/VERSION" % repo):
    ref = path.split("?ref=", 1)[1] if "?ref=" in path else ""
    candidate = fix / ("version.%s" % ref)
    if not candidate.exists():
        not_found()
    raw = candidate.read_text(encoding="utf-8")
    emit({"encoding": "base64",
          "content": base64.b64encode(raw.encode()).decode() + "\n"})

if path.startswith("repos/%s/commits/" % repo):
    ref = path.rsplit("/", 1)[1]
    candidate = fix / ("commit.%s.json" % ref)
    if not candidate.exists():
        not_found()
    emit(json.loads(candidate.read_text(encoding="utf-8")))

if path.startswith("repos/%s/git/ref/tags/" % repo):
    if (fix / "tag_exists").exists():
        emit({"ref": "refs/tags/served"})
    not_found()

sys.stderr.write("unexpected gh api path: %s\n" % path)
sys.exit(2)
STUB
chmod +x "$stub_bin/gh"

# --- fixtures ----------------------------------------------------------------
fresh() {
  rm -rf "$fix"
  mkdir -p "$fix"
  : >"$fix/calls.log"
  jq -nc --arg sha "$MERGE" \
    '{state: "MERGED", baseRefName: "main", mergeCommit: {oid: $sha}}' >"$fix/pr.json"
  jq -nc --arg sha "$MERGE" --arg parent "$PARENT" \
    '{sha: $sha, parents: [{sha: $parent}]}' >"$fix/commit.$MERGE.json"
  printf '0.103.0' >"$fix/version.$PARENT"
  printf '0.104.0' >"$fix/version.$MERGE"
}

run_case() { # expected_exit [args...]
  local want=$1 got
  shift
  set +e
  FIX="$fix" FIX_REPO="$REPO" PATH="$stub_bin:$PATH" \
    bash "$script" "$@" >"$tmp/out" 2>"$tmp/err"
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

# 1. The hole: the merge landed, it bumped VERSION, and no tag exists -> start
#    the release path with a dispatch of auto-tag on main.
fresh
run_case 0 "$PR" --repo "$REPO"
expect_output "bumped VERSION 0.103.0 -> 0.104.0"
expect_output "Dispatched auto-tag.yml on main for v0.104.0"
expect_dispatches 1
if grep -qx 'workflow run auto-tag.yml --repo '"$REPO"' --ref main' "$fix/calls.log"; then
  ok "dispatched on the release branch"
else
  bad "dispatch did not target main: $(cat "$fix/calls.log")"
fi

# 2. A merge that did not change VERSION must never start a release job.
fresh
printf '0.104.0' >"$fix/version.$PARENT"
run_case 0 "$PR" --repo "$REPO"
expect_output "did not change VERSION"
expect_dispatches 0

# 3. The tag already exists: auto-tag would no-op, so do not dispatch at all.
fresh
: >"$fix/tag_exists"
run_case 0 "$PR" --repo "$REPO"
expect_output "tag v0.104.0 already exists"
expect_dispatches 0

# 4. The merge has not landed yet: the helper waits, then leaves it to the
#    schedule. It must not fail the gate run for a PR that is still queued.
fresh
jq -nc '{state: "OPEN", baseRefName: "main", mergeCommit: null}' >"$fix/pr.json"
run_case 0 "$PR" --repo "$REPO" --wait 0
expect_output "still OPEN after 0s"
expect_output "release-tag-watchdog"
expect_dispatches 0

# 5. Closed without a merge: nothing to release, and not an error.
fresh
jq -nc '{state: "CLOSED", baseRefName: "main", mergeCommit: null}' >"$fix/pr.json"
run_case 0 "$PR" --repo "$REPO"
expect_output "closed without a merge"
expect_dispatches 0

# 6. Merged into a branch that is not the release branch: its VERSION is not a
#    release.
fresh
jq -nc --arg sha "$MERGE" \
  '{state: "MERGED", baseRefName: "release/1.x", mergeCommit: {oid: $sha}}' >"$fix/pr.json"
run_case 0 "$PR" --repo "$REPO" --base main
expect_output "merged into release/1.x, not main"
expect_dispatches 0

# 7. A failed dispatch is loud: the gate prints it as a warning and the
#    schedule remains the backstop.
fresh
printf '1' >"$fix/dispatch_exit"
run_case 1 "$PR" --repo "$REPO"
expect_dispatches 1

# 8. A merged PR reporting no merge commit is unreadable state, not "no change".
fresh
jq -nc '{state: "MERGED", baseRefName: "main", mergeCommit: null}' >"$fix/pr.json"
run_case 1 "$PR" --repo "$REPO"
expect_output "reports no merge commit"
expect_dispatches 0

# 9. A merge commit with no parent cannot be diffed: fail instead of guessing
#    that VERSION did not change.
fresh
jq -nc --arg sha "$MERGE" '{sha: $sha, parents: []}' >"$fix/commit.$MERGE.json"
run_case 1 "$PR" --repo "$REPO"
expect_output "has no parent"
expect_dispatches 0

# 10. An unreadable VERSION at the merge commit is unreadable state too.
fresh
rm -f "$fix/version.$MERGE"
run_case 1 "$PR" --repo "$REPO"
expect_output "could not read VERSION"
expect_dispatches 0

# 11. Usage errors: a PR number is required, and --wait takes seconds.
fresh
run_case 2 --repo "$REPO"
run_case 2 "$PR" --repo "$REPO" --wait soon
expect_dispatches 0

if [[ $fail -ne 0 ]]; then
  printf 'FAIL [dispatch-release-tag]: %d checks, at least one failed\n' "$checks" >&2
  exit 1
fi
printf 'PASS [dispatch-release-tag]: %d checks\n' "$checks"
