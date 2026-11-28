#!/usr/bin/env bash
# Hermetic regression suite for check-secret-environments.py (#1232).
#
# WHY. The guard exists to keep one judgement honest: "does this job read a
# signing/App secret from the environment that owns it, or from repo level,
# where any workflow a pushed branch can add would read it too?". Get it
# permissive and the exposure comes back silently; get it strict and every
# compliant job is reported, so lanes learn to ignore the gate and the shrinking
# baseline stops being deleted from.
#
# Every case builds a throwaway tree (no network, no credentials, no git), runs
# the guard with --root/--baseline, and asserts the exit status plus the
# reported evidence. The last cases keep the guard wired into
# scripts/preflight/all.sh (a guard nobody wires is a guard nobody runs) and run
# the real tree against its real baseline, which is the checklist of jobs that
# still need `workflows: write` to take the `environment:` line.
#
# Usage:
#   .github/scripts/check-secret-environments.test.sh
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
guard="$here/check-secret-environments.py"
real_root=$(cd "$here/../.." && pwd)
all_sh="$real_root/scripts/preflight/all.sh"

command -v python3 >/dev/null 2>&1 || {
  printf 'FAIL [secret-environments]: python3 is required\n' >&2
  exit 2
}

failures=0
checks=0
out=""
status=0
ok() {
  checks=$((checks + 1))
  printf 'ok   [secret-environments]: %s\n' "$1"
}
no() {
  checks=$((checks + 1))
  failures=$((failures + 1))
  printf 'FAIL [secret-environments]: %s\n' "$1" >&2
  printf '%s\n' "$out" | sed 's/^/    /' >&2
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
repo=""

# new_repo <name>: a fresh tree with an empty baseline.
new_repo() {
  repo="$tmp/$1"
  mkdir -p "$repo/.github/workflows"
  : > "$repo/baseline"
}

put() { # put <path>  (content on stdin)
  mkdir -p "$repo/$(dirname "$1")"
  cat > "$repo/$1"
}

run_guard() {
  out=$(python3 "$guard" --root "$repo" --baseline "$repo/baseline" "$@" 2>&1)
  status=$?
}

expect_status() { # expect_status <want> <label>
  if [[ "$status" == "$1" ]]; then
    ok "$2"
  else
    no "$2: exit $status, want $1"
  fi
}

expect_out() { # expect_out <literal> <label>
  if printf '%s' "$out" | grep -Fq -- "$1"; then
    ok "$2"
  else
    no "$2: output lacks '$1'"
  fi
}

# ---------------------------------------------------------------------------
# A violating job: the sparkle key read from repo level.
# ---------------------------------------------------------------------------
new_repo missing
put .github/workflows/build.yml <<'EOF'
name: build
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - env:
          SPARKLE_PRIVATE_KEY: ${{ secrets.SPARKLE_PRIVATE_KEY }}
        run: echo sign
EOF
run_guard
expect_status 1 "a job without the environment is reported"
expect_out "build.yml:8" "the finding names the referencing line"
expect_out 'job `build`' "the finding names the job"
expect_out 'expected environment `signing`' "the finding names the environment"

# ---------------------------------------------------------------------------
# The same job with its shrinking-baseline entry is accepted, and nothing else
# is: the entry is keyed by (path, job, environment).
# ---------------------------------------------------------------------------
printf '.github/workflows/build.yml\tbuild\tsigning\n' > "$repo/baseline"
run_guard
expect_status 0 "a baselined job passes"
expect_out "1 baselined job(s)" "the pass line counts the baselined job"

printf '.github/workflows/build.yml\tbuild\tdev-dispatch\n' > "$repo/baseline"
run_guard
expect_status 1 "a baseline entry for the wrong environment does not cover it"

# ---------------------------------------------------------------------------
# A stale entry fails: the job now declares its environment, so the entry must
# be deleted in the same PR (the list can only shrink).
# ---------------------------------------------------------------------------
new_repo fixed
put .github/workflows/build.yml <<'EOF'
name: build
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    environment: signing
    steps:
      - env:
          SPARKLE_PRIVATE_KEY: ${{ secrets.SPARKLE_PRIVATE_KEY }}
        run: echo sign
EOF
run_guard
expect_status 0 "a job that declares its environment passes with an empty baseline"

printf '.github/workflows/build.yml\tbuild\tsigning\n' > "$repo/baseline"
run_guard
expect_status 1 "a stale baseline entry fails"
expect_out "stale baseline" "the stale failure says so"

# ---------------------------------------------------------------------------
# The mapping form of `environment:` resolves too.
# ---------------------------------------------------------------------------
new_repo mapping
put .github/workflows/build.yml <<'EOF'
name: build
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    environment:
      name: signing
      url: https://example.com
    steps:
      - run: echo "${{ secrets.CERTIFICATE_PASSWORD }}"
EOF
run_guard
expect_status 0 "environment: + nested name: is honoured"

# ---------------------------------------------------------------------------
# A wrong environment, a workflow-level env: and a comment are judged.
# ---------------------------------------------------------------------------
new_repo wrong
put .github/workflows/build.yml <<'EOF'
name: build
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    environment: dev-dispatch
    steps:
      - run: echo "${{ secrets.CERTIFICATE_BASE64 }}"
EOF
run_guard
expect_status 1 "an environment that does not own the secret is reported"
expect_out 'expected environment `signing`' "the wrong-environment finding names the owner"

new_repo workflow-level
put .github/workflows/build.yml <<'EOF'
name: build
on: [push]
env:
  SPARKLE_PRIVATE_KEY: ${{ secrets.SPARKLE_PRIVATE_KEY }}
jobs:
  build:
    runs-on: ubuntu-latest
    environment: signing
    steps:
      - run: echo sign
EOF
run_guard
expect_status 1 "a workflow-level env: reference is reported"
expect_out "outside any job" "the workflow-level finding says it is outside a job"

new_repo comment
put .github/workflows/build.yml <<'EOF'
name: build
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    # was: secrets.SPARKLE_PRIVATE_KEY here before #1232
    steps:
      - run: echo "${{ secrets.GITHUB_TOKEN }}"
EOF
run_guard
expect_status 0 "a comment naming a secret is inert"

# ---------------------------------------------------------------------------
# The guard's own scanner fixtures.
# ---------------------------------------------------------------------------
out=$(python3 "$guard" --self-test 2>&1)
status=$?
expect_status 0 "the scanner self-test passes"
expect_out "self-test OK" "the self-test reports success"

# ---------------------------------------------------------------------------
# Wiring: a guard nobody wires is a guard nobody runs. The required `ci` job
# executes scripts/preflight/all.sh, so the guard must be invoked from there
# (a workflow-file step is what the missing `workflows` permission blocks).
# ---------------------------------------------------------------------------
if grep -Fq "check-secret-environments.py" "$all_sh"; then
  ok "the guard is wired into scripts/preflight/all.sh"
else
  out=""
  no "the guard is not wired into scripts/preflight/all.sh"
fi

# ---------------------------------------------------------------------------
# The real tree against the real baseline: every remaining finding is one of
# the four known jobs, and the baseline has no stale entry.
# ---------------------------------------------------------------------------
out=$(cd "$real_root" && python3 "$guard" 2>&1)
status=$?
expect_status 0 "the real tree passes against its baseline"
expect_out "4 baselined job(s)" "the four blocked jobs are the whole baseline"

if (( failures > 0 )); then
  printf 'check-secret-environments.test: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'check-secret-environments.test: passed (%d checks)\n' "$checks"
