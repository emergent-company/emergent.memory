#!/usr/bin/env bash
# Hermetic regression suite for the merge-guard workflow's trigger-dependent
# verdict (#886).
#
# WHY: a push publishes a **neutral** `merge-guard: awaiting review` check while
# the `workflow_dispatch` run the merge wrapper performs stays red for "no
# marker" and "stale marker", and a trusted verdict that rejects the head is red
# on either trigger. Every one of those is a string comparison in the workflow,
# and both mistakes are silent in the wrong direction:
#
#   * a wrong branch in `awaiting_review()` turns a rejection green -- a merge
#     the guard exists to stop;
#   * the opposite mistake puts red back on every push, the noise that made
#     lanes ignore the guard (#875);
#   * `MODE` is the whole distinction between "automatic evaluation" and
#     "binding evaluation", and it is one comparison away from being inverted.
#
# `bash -n` and a YAML parse catch none of that, so this suite extracts BOTH
# `run:` blocks from the workflow at run time, replaces `gh` with a stub on
# PATH, and drives the eight cases (no marker / stale marker / approval on each
# trigger, rejection on both), asserting the step exit status, the step outputs
# and the published check conclusion.
#
# The workflow lives in THIS repo, next to the harness: it is resolved relative
# to the repository root, and a missing file is a hard failure -- there is no
# skip path. The suite is wired into the required `ci` job in
# `.github/workflows/ci.yml`, so a pull request runs it.
#
# The extraction is deliberately strict: a third step, a second `run:` block in
# one step, a renamed env input or an expression this harness cannot resolve
# fails the suite instead of silently testing a stale copy of the logic.
#
# Offline, deterministic, no credentials: the stub serves the PR head and the
# comment list from fixtures, and records the check-run body the publish step
# would have POSTed.
#
# Usage:
#   .github/scripts/merge-guard-awaiting.test.sh
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(cd "$here/../.." && pwd)
workflow="$root/.github/workflows/merge-guard.yml"

if [[ ! -f $workflow ]]; then
  printf 'FAIL [merge-guard-awaiting]: workflow not found at %s\n' "$workflow" >&2
  exit 1
fi
for cmd in jq python3; do
  command -v "$cmd" >/dev/null 2>&1 || {
    printf 'FAIL [merge-guard-awaiting]: %s is required\n' "$cmd" >&2
    exit 2
  }
done

# Fixture inputs shared by every case. `HEAD` is the fixture PR head; the
# workflow's own HEAD env is filled from it for a push, and fetched from the
# stub for a dispatch (which is what the real `workflow_dispatch` run does).
REPO=emergent-company/emergent.memory
PR=7
TRUSTED_IDS=1051101,4037961,188950
RUN_ID=41927694911
HEAD=6ae4c95cf07ef2f5eb17e9c928ac02c72174ddcb
STALE=0d5ca9d4aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
NOW=2026-11-06T10:00:00Z

# Values `resolve_env` may read before a case starts (the validation pass
# below), so `set -u` never trips on a per-case variable.
HEAD_ENV=$HEAD
MODE=pull_request_target
OUTCOME=success
AWAITING=
SUMMARY_PATH=

checks_n=0
fail=0
ok() { checks_n=$((checks_n + 1)); printf 'ok [merge-guard-awaiting]: %s\n' "$1"; }
bad() {
  checks_n=$((checks_n + 1))
  printf 'FAIL [merge-guard-awaiting]: %s\n' "$1" >&2
  fail=1
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
STUB_BIN="$tmp/bin"
STUB_DIR="$tmp/state"
STEP_DIR="$tmp/steps"
mkdir -p "$STUB_BIN" "$STUB_DIR" "$STEP_DIR"

# --- extract the two run blocks and their env maps ---------------------------
# A tiny, strict reader rather than a YAML dependency: the workflow's steps are
# a flat list under `jobs.verify.steps`, every step starts at six spaces with
# `- name:`, and each `run: |` body is its block scalar. Anything unexpected is
# a hard failure, because the point of this suite is to bite on change.
if ! python3 - "$workflow" "$STEP_DIR" >"$tmp/extract.log" 2>&1 <<'PY'
import pathlib
import re
import sys

workflow = pathlib.Path(sys.argv[1])
out = pathlib.Path(sys.argv[2])
lines = workflow.read_text(encoding="utf-8").splitlines()

starts = [i for i, l in enumerate(lines) if re.match(r"^      - name: ", l)]
items = [i for i, l in enumerate(lines) if re.match(r"^      - ", l)]
if len(starts) != len(items):
    sys.exit(f"unsupported step list: {len(items)} list items, {len(starts)} '- name:' steps")
if len(starts) != 2:
    sys.exit(f"expected exactly 2 steps under jobs.verify.steps, found {len(starts)}")

# End of the steps list: the first non-blank line after the first step at
# indent <= 4. Guarding on it keeps a later top-level key out of step 2.
end = len(lines)
for i in range(starts[0], len(lines)):
    l = lines[i]
    if l.strip() and len(l) - len(l.lstrip(" ")) <= 4:
        end = i
        break
if not any(re.match(r"^    steps:\s*$", l) for l in lines[: starts[0]]):
    sys.exit("jobs.verify.steps not found")
starts.append(end)

blocks = []
for n, (a, b) in enumerate(zip(starts, starts[1:]), start=1):
    body = lines[a:b]
    run_keys = [i for i, l in enumerate(body) if re.match(r"^        run: ", l)]
    if len(run_keys) != 1:
        sys.exit(f"step {n}: expected exactly one 'run:' key, found {len(run_keys)}")
    run_at = run_keys[0]
    if body[run_at] != "        run: |":
        sys.exit(f"step {n}: run is not a literal block scalar: {body[run_at]!r}")

    indent = None
    run = []
    for l in body[run_at + 1:]:
        if l.strip() == "":
            run.append("")
            continue
        cur = len(l) - len(l.lstrip(" "))
        if indent is None:
            indent = cur
        if cur < indent:
            break
        run.append(l[indent:])
    while run and run[-1] == "":
        run.pop()

    env = []
    env_at = None
    for i, l in enumerate(body):
        if l == "        env:":
            env_at = i
            break
    if env_at is not None:
        for l in body[env_at + 1:]:
            if l.strip() == "":
                continue
            cur = len(l) - len(l.lstrip(" "))
            if cur <= 8:
                break
            if l.lstrip().startswith("#"):
                continue
            key_m = re.match(r"^          ([A-Za-z_][A-Za-z0-9_]*): (.*)$", l)
            if not key_m:
                sys.exit(f"step {n}: unparsed env line: {l!r}")
            value = key_m.group(2).strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "'\"":
                value = value[1:-1]
            expr_m = re.match(r"^\$\{\{(.*)\}\}$", value)
            if expr_m:
                value = expr_m.group(1)
            value = re.sub(r"\s+", " ", value).strip()
            env.append((key_m.group(1), value))
    blocks.append((run, env))

for n, (run, env) in enumerate(blocks, start=1):
    (out / f"step{n}.sh").write_text("\n".join(run) + "\n", encoding="utf-8")
    (out / f"step{n}.env").write_text(
        "".join(f"{k}={v}\n" for k, v in env), encoding="utf-8"
    )
print(f"extracted {len(blocks)} run blocks")
PY
then
  bad "the workflow's steps are readable as exactly two steps with one run block each ($(cat "$tmp/extract.log"))"
  echo "merge-guard-awaiting: FAILED" >&2
  exit 1
fi
ok "the workflow yields exactly two steps with one run block each"

# An env input this suite cannot resolve is a hard failure: running the block
# with a wrong value would test the harness, not the workflow.
resolve_env() { # <envfile>  -> KEY=VALUE lines on stdout (fails on unknown)
  local line key expr val
  while IFS= read -r line || [[ -n $line ]]; do
    [[ -n $line ]] || continue
    key=${line%%=*}
    expr=${line#*=}
    case $expr in
      'github.token') val=dummy-token ;;
      'github.repository') val=$REPO ;;
      'github.event.pull_request.number || inputs.pr') val=$PR ;;
      "github.event.pull_request.head.sha || ''") val=${HEAD_ENV-} ;;
      'vars.MERGE_GUARD_TRUSTED_USER_IDS') val=$TRUSTED_IDS ;;
      'github.event_name') val=$MODE ;;
      'steps.verify.outcome') val=$OUTCOME ;;
      'steps.verify.outputs.awaiting') val=$AWAITING ;;
      'steps.verify.outputs.summary_path') val=$SUMMARY_PATH ;;
      'github.run_id') val=$RUN_ID ;;
      *)
        printf 'unresolved env %s=%s\n' "$key" "$expr" >&2
        return 1
        ;;
    esac
    printf '%s=%s\n' "$key" "$val"
  done <"$1"
}

# A renamed or added env input changes the set, and the harness must notice
# instead of quietly feeding the step a different variable.
assert_env_keys() { # <step> <envfile> <expected newline-separated, sorted>
  local step=$1 envfile=$2 expected=$3 got
  got=$(cut -d= -f1 "$envfile" | sort)
  if [[ $got == "$expected" ]]; then
    ok "step ${step}: env inputs are the expected set"
  else
    bad "step ${step}: env inputs changed (got: $(printf '%s' "$got" | tr '\n' ' '); expected: $(printf '%s' "$expected" | tr '\n' ' '))"
  fi
}

if ! resolve_env "$STEP_DIR/step1.env" >"$tmp/env1.resolved" 2>"$tmp/env1.err" ||
  ! resolve_env "$STEP_DIR/step2.env" >"$tmp/env2.resolved" 2>"$tmp/env2.err"; then
  bad "every env input of both steps is one this suite resolves ($(cat "$tmp/env1.err" "$tmp/env2.err" 2>/dev/null))"
  echo "merge-guard-awaiting: FAILED" >&2
  exit 1
fi
ok "every env input of both steps resolves"

assert_env_keys 1 "$STEP_DIR/step1.env" $'GH_TOKEN\nHEAD\nMODE\nPR\nREPO\nTRUSTED_IDS'
assert_env_keys 2 "$STEP_DIR/step2.env" $'AWAITING\nGH_TOKEN\nHEAD\nOUTCOME\nPR\nREPO\nSUMMARY_PATH'

# --- stub gh -----------------------------------------------------------------
cat >"$STUB_BIN/gh" <<'STUB'
#!/usr/bin/env bash
# Deterministic `gh` stand-in for the merge-guard steps. Fixtures in
# $GH_STUB_DIR:
#   head        the PR head SHA
#   comments    a JSON array of PR comments (the API's own payload)
#   check.json  the check-run body the publish step POSTs (written here)
#   calls       every invocation, so the API surface the workflow uses is pinned
set -euo pipefail
: "${GH_STUB_DIR:?}"
printf '%s\n' "$*" >>"$GH_STUB_DIR/calls"
[[ ${1:-} == api ]] || {
  printf 'stub gh: unhandled command: %s\n' "$*" >&2
  exit 1
}
shift
case "$*" in
  *check-runs*)
    cat >"$GH_STUB_DIR/check.json"
    ;;
  *'/issues/'*'/comments'*)
    jq -c '.[]' "$GH_STUB_DIR/comments"
    ;;
  *'/pulls/'*)
    cat "$GH_STUB_DIR/head"
    ;;
  *)
    printf 'stub gh: unhandled api call: %s\n' "$*" >&2
    exit 1
    ;;
esac
STUB
chmod +x "$STUB_BIN/gh"
export PATH="$STUB_BIN:$PATH"
export GH_STUB_DIR="$STUB_DIR"

# --- fixtures ----------------------------------------------------------------
# Each fixture is the PR's comment list. `author_association: OWNER` is trusted
# without the allowlist, so the fixtures exercise the selector rather than the
# trust route (which scripts/merge-reviewed-pr-test.sh already pins).
fixture() { # <file> <body...>
  local file=$1
  shift
  : >"$file"
  local sep=""
  printf '[' >"$file"
  local body
  for body in "$@"; do
    printf '%s{"user":{"id":4037961},"author_association":"OWNER","created_at":"%s","html_url":"https://github.com/%s/pull/%s#issuecomment-1","body":%s}' \
      "$sep" "$NOW" "$REPO" "$PR" "$(jq -Rn --arg b "$body" '$b')" >>"$file"
    sep=","
  done
  printf ']' >>"$file"
}

fixture "$tmp/no-marker.json"
fixture "$tmp/stale.json" "$(printf 'Reviewed-SHA: %s\nVerdict: MERGE' "$STALE")"
fixture "$tmp/approval.json" "$(printf 'Reviewed-SHA: %s\nVerdict: MERGE' "$HEAD")"
fixture "$tmp/rejection.json" "$(printf 'Reviewed-SHA: %s\nVerdict: CHANGES_REQUESTED' "$HEAD")"

# --- one case ----------------------------------------------------------------
step1_status=0
step2_status=0

run_case() { # <label> <fixture> <mode> <want-step1> <want-awaiting:true|false> <want-conclusion> <want-title> <want-step2>
  local label=$1 fixture=$2 mode=$3
  local want1=$4 want_awaiting=$5 want_conclusion=$6 want_title=$7 want2=$8
  local runner="$tmp/runner" out1="$tmp/out1" summary="$tmp/summary" check="$STUB_DIR/check.json"
  local env1=() env2=()

  rm -rf "$STUB_DIR"
  mkdir -p "$STUB_DIR"
  cp "$fixture" "$STUB_DIR/comments"
  printf '%s\n' "$HEAD" >"$STUB_DIR/head"
  : >"$STUB_DIR/calls"
  rm -rf "$runner" "$out1" "$summary" "$check"
  mkdir -p "$runner"
  : >"$out1"
  : >"$summary"

  MODE=$mode
  if [[ $mode == workflow_dispatch ]]; then
    # Real dispatch runs carry no pull_request payload, so the step fetches the
    # head through the API. Exercise that path.
    HEAD_ENV=
  else
    HEAD_ENV=$HEAD
  fi

  resolve_env "$STEP_DIR/step1.env" >"$tmp/env1.resolved"
  mapfile -t env1 <"$tmp/env1.resolved"

  set +e
  env "${env1[@]}" GH_STUB_DIR="$STUB_DIR" \
    RUNNER_TEMP="$runner" GITHUB_OUTPUT="$out1" GITHUB_STEP_SUMMARY="$summary" \
    bash "$STEP_DIR/step1.sh" >"$tmp/step1.log" 2>&1
  step1_status=$?
  set -e

  AWAITING=$(sed -n 's/^awaiting=//p' "$out1" | tail -n 1)
  SUMMARY_PATH=$(sed -n 's/^summary_path=//p' "$out1" | tail -n 1)
  if [[ $step1_status == 0 ]]; then OUTCOME=success; else OUTCOME=failure; fi

  resolve_env "$STEP_DIR/step2.env" >"$tmp/env2.resolved"
  mapfile -t env2 <"$tmp/env2.resolved"

  set +e
  env "${env2[@]}" GH_STUB_DIR="$STUB_DIR" GITHUB_RUN_ID="$RUN_ID" \
    GITHUB_STEP_SUMMARY="$summary" GITHUB_OUTPUT="$out1" \
    bash "$STEP_DIR/step2.sh" >"$tmp/step2.log" 2>&1
  step2_status=$?
  set -e

  if [[ $step1_status == "$want1" ]]; then
    ok "$label: verify exits $want1"
  else
    bad "$label: verify exits $want1 (got $step1_status; $(tail -3 "$tmp/step1.log"))"
  fi

  if [[ $want_awaiting == true ]]; then
    if [[ $AWAITING == true ]]; then
      ok "$label: verify publishes awaiting=true"
    else
      bad "$label: verify publishes awaiting=true (got '${AWAITING:-<absent>}')"
    fi
  else
    if [[ -z $AWAITING ]]; then
      ok "$label: verify does not publish awaiting"
    else
      bad "$label: verify does not publish awaiting (got '$AWAITING')"
    fi
  fi

  local conclusion title
  conclusion=$(jq -r '.conclusion // "<absent>"' "$check" 2>/dev/null || echo '<no-body>')
  title=$(jq -r '.output.title // "<absent>"' "$check" 2>/dev/null || echo '<no-body>')
  if [[ $conclusion == "$want_conclusion" ]]; then
    ok "$label: published conclusion is $want_conclusion"
  else
    bad "$label: published conclusion is $want_conclusion (got $conclusion; $(tail -3 "$tmp/step2.log"))"
  fi
  if [[ $title == "$want_title" ]]; then
    ok "$label: published title is '$want_title'"
  else
    bad "$label: published title is '$want_title' (got '$title')"
  fi
  if [[ $step2_status == "$want2" ]]; then
    ok "$label: publish exits $want2"
  else
    bad "$label: publish exits $want2 (got $step2_status; $(tail -3 "$tmp/step2.log"))"
  fi
}

# The eight cases: a push is an automatic evaluation (neutral while nothing can
# be enforced yet), a dispatch is the binding one (red -- nothing merges
# unreviewed), and a rejection is red on both.
run_case "push, no marker" "$tmp/no-marker.json" pull_request_target \
  0 true neutral "merge-guard: awaiting review" 0
run_case "push, stale marker" "$tmp/stale.json" pull_request_target \
  0 true neutral "merge-guard: awaiting review" 0
run_case "push, approval" "$tmp/approval.json" pull_request_target \
  0 false success "merge-guard: pass" 0

run_case "dispatch, no marker" "$tmp/no-marker.json" workflow_dispatch \
  1 false failure "merge-guard: failure" 1
run_case "dispatch, stale marker" "$tmp/stale.json" workflow_dispatch \
  1 false failure "merge-guard: failure" 1
run_case "dispatch, approval" "$tmp/approval.json" workflow_dispatch \
  0 false success "merge-guard: pass" 0

run_case "push, rejection" "$tmp/rejection.json" pull_request_target \
  1 false failure "merge-guard: failure" 1
run_case "dispatch, rejection" "$tmp/rejection.json" workflow_dispatch \
  1 false failure "merge-guard: failure" 1

if ((fail)); then
  echo "merge-guard-awaiting: FAILED" >&2
  exit 1
fi
printf 'merge-guard-awaiting: passed (%d checks)\n' "$checks_n"
