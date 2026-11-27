#!/usr/bin/env bash
# Hermetic regression suite for fetch-hygiene.sh (#1158, follow-up to #956).
#
# WHY. The guard exists to keep one judgement honest: "does this fetch name the
# ref it wants, or does it leave git free to resolve a name that refs/tags/dev
# also answers?". Get it permissive and the bare-name form comes back, and a
# lane that fetches `origin/dev` reads the connector's rolling dev tag while
# `refs/remotes/origin/dev` stays stale -- the failure #956 and #1158 both
# chased. Get it strict and every compliant fetch is reported, so lanes learn to
# ignore the gate.
#
# Every case builds a throwaway git repository (no remote, no network, git
# only), writes the shape under test, and asserts exit status plus the reported
# evidence. The last cases keep the guard wired into scripts/preflight/all.sh
# (the required `ci` job runs a guard nobody wired = a guard nobody runs) and
# check the real tree against its baseline, which is the shrinking checklist for
# #1158's four workflow sites.
#
# Usage:
#   scripts/preflight/fetch-hygiene.test.sh
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/fetch-hygiene.sh"
real_root=$(cd "$here/../.." && pwd)
all_sh="$here/all.sh"

command -v git >/dev/null 2>&1 || {
  printf 'FAIL [fetch-hygiene]: git is required\n' >&2
  exit 2
}

failures=0
checks=0
out=""
status=0
ok() {
  checks=$((checks + 1))
  printf 'ok   [fetch-hygiene]: %s\n' "$1"
}
no() {
  checks=$((checks + 1))
  failures=$((failures + 1))
  printf 'FAIL [fetch-hygiene]: %s\n' "$1" >&2
  printf '%s\n' "$out" | sed 's/^/    /' >&2
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
repo=""

# new_repo <name>: a fresh git repository with an empty baseline.
new_repo() {
  repo="$tmp/$1"
  mkdir -p "$repo/.github/workflows" "$repo/scripts"
  git -C "$repo" init -q
  git -C "$repo" config user.email test@example.com
  git -C "$repo" config user.name test
  : > "$repo/baseline"
}

put() { # put <path> <<'EOF' ... EOF  (content on stdin)
  mkdir -p "$repo/$(dirname "$1")"
  cat > "$repo/$1"
}

commit_all() {
  git -C "$repo" add -A >/dev/null
  git -C "$repo" commit -qm fixture
}

run_guard() { # run_guard [args...]
  out=$(FETCH_HYGIENE_ROOT="$repo" bash "$script" "$@" 2>&1)
  status=$?
}

expect_status() { # expect_status <want> <label>
  if [[ "$status" == "$1" ]]; then
    ok "$2"
  else
    no "$2 (status $status, want $1)"
  fi
}

expect_contains() { # expect_contains <label> <needle>
  if [[ "$out" == *"$2"* ]]; then
    ok "$1"
  else
    no "$1 (missing: $2)"
  fi
}

# ---------------------------------------------------------------------------
# 1. The exact #1158 shape: a workflow step, indented shell, quoted variable,
#    redirection and `|| true`. It must be reported with file and line.
# ---------------------------------------------------------------------------
new_repo workflow-shape
put .github/workflows/ci.yml <<'EOF'
name: ci
on: [pull_request]
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - run: |
          git fetch --no-tags origin "${GITHUB_BASE_REF}" 2>/dev/null || true
          echo ok
EOF
commit_all
run_guard
expect_status 1 "a workflow's bare-name base fetch fails the guard"
expect_contains "the site is named with its line" ".github/workflows/ci.yml:8:"
expect_contains "the fix is spelled out" "refs/remotes/origin/<branch>"

# ---------------------------------------------------------------------------
# 2. The same site, baselined (with comments and blank lines, which the parser
#    must ignore): the gate passes. The baseline is the mechanism #1158 relies
#    on to land the enforcement half before the workflow half.
# ---------------------------------------------------------------------------
cat > "$repo/baseline" <<'EOF'
# known-unfixed sites

.github/workflows/ci.yml	git fetch --no-tags origin "${GITHUB_BASE_REF}" 2>/dev/null || true
EOF
run_guard --baseline "$repo/baseline"
expect_status 0 "a baselined site passes"
expect_contains "the pass names how many are baselined" "1 baselined site(s)"

# ---------------------------------------------------------------------------
# 3. A baselined site that no longer exists is an error: that is what stops the
#    list from rotting into a permanent allowlist (and what makes the workflow
#    fix and its baseline deletion land in one PR).
# ---------------------------------------------------------------------------
new_repo stale-baseline
put scripts/fetch.sh <<'EOF'
echo hi
EOF
commit_all
printf '.github/workflows/gone.yml\tgit fetch origin dev\n' > "$repo/baseline"
run_guard --baseline "$repo/baseline"
expect_status 1 "a stale baseline entry fails the guard"
expect_contains "the stale entry is named" "stale baseline entr(ies)"
expect_contains "the entry to delete is shown" ".github/workflows/gone.yml"

# ---------------------------------------------------------------------------
# 4. Shapes that are already correct, or not a fetch at all. None may be
#    reported: a strict gate that cries wolf gets bypassed.
# ---------------------------------------------------------------------------
new_repo compliant
put scripts/fetch.sh <<'EOF'
#!/usr/bin/env bash
git fetch origin
git fetch --all
git fetch --no-tags --prune origin "+refs/heads/dev:refs/remotes/origin/dev"
git fetch --quiet --no-tags origin "+refs/heads/$b:refs/remotes/origin/$b" 2>/dev/null
if ! git fetch --quiet --no-tags origin "+refs/heads/$b:refs/remotes/origin/$b" 2>/dev/null; then
  echo "could not reach origin"
fi
git fetch origin tag v1.2.3
git fetch origin refs/heads/dev
git fetch origin dev:dev
git fetch origin 0123456789abcdef0123456789abcdef01234567
git fetch origin HEAD
# git fetch origin dev
echo "git fetch origin dev"
printf '%s\n' 'run: git fetch origin dev'
exit 0
EOF
commit_all
run_guard --list
expect_status 0 "compliant and non-fetch lines pass"
expect_contains "nothing is listed" "no bare-name fetch sites"

# ---------------------------------------------------------------------------
# 5. The bare-name shapes that must be caught: a literal branch, a quoted
#    variable, a continuation line, an `if !` prefix, and a YAML inline run.
# ---------------------------------------------------------------------------
new_repo bare-shapes
put scripts/fetch.sh <<'EOF'
#!/usr/bin/env bash
git fetch origin dev
git fetch origin "${BASE_BRANCH}"
git fetch origin \
  gh-pages
if ! git fetch origin main; then exit 1; fi
EOF
put .github/workflows/inline.yml <<'EOF'
name: inline
on: [push]
jobs:
  t:
    runs-on: ubuntu-latest
    steps:
      - name: x
        run: git fetch origin dev
EOF
commit_all
run_guard
expect_status 1 "every bare-name shape fails"
expect_contains "a literal branch is caught" "scripts/fetch.sh:2:"
expect_contains "a quoted variable is caught" "scripts/fetch.sh:3:"
expect_contains "a continuation is caught at its first line" "scripts/fetch.sh:4:"
expect_contains "an if-not prefix is caught" "scripts/fetch.sh:6:"
expect_contains "an inline run-step is caught" ".github/workflows/inline.yml:8:"
if [[ "$(grep -c '^  scripts/fetch.sh:' <<< "$out")" == 4 ]]; then
  ok "each shell site is reported once, not once per ref"
else
  no "each shell site is reported once, not once per ref"
fi

# ---------------------------------------------------------------------------
# 6. Duplicate lines are counted, not deduplicated: one baseline entry cannot
#    cover a second copy of the same bad line.
# ---------------------------------------------------------------------------
new_repo duplicates
put scripts/fetch.sh <<'EOF'
git fetch origin dev
git fetch origin dev
EOF
commit_all
printf 'scripts/fetch.sh\tgit fetch origin dev\n' > "$repo/baseline"
run_guard --baseline "$repo/baseline"
expect_status 1 "the copy is not covered by one baseline entry"
expect_contains "the uncovered copy is reported" "scripts/fetch.sh:2:"

# ---------------------------------------------------------------------------
# 7. An array expansion can hide any number of words, so the line is skipped
#    rather than guessed at (release-tag.sh passes its flags this way).
# ---------------------------------------------------------------------------
new_repo array-expansion
put scripts/fetch.sh <<'EOF'
git fetch "${fetch_args[@]}" "$remote" "+refs/heads/x:refs/remotes/origin/x"
EOF
commit_all
run_guard --list
expect_status 0 "an unknown-arity expansion is skipped, not flagged"
expect_contains "the line is not listed" "no bare-name fetch sites"

# ---------------------------------------------------------------------------
# 7b. The exemption is exactly one file: the guard's own suite (whose heredocs
#     are fixture repositories, not commands). A sibling *.test.sh that runs a
#     bare fetch for real is still reported, so the exemption cannot be read as
#     "skip tests".
# ---------------------------------------------------------------------------
new_repo sibling-tests
put scripts/helper.test.sh <<'EOF'
git fetch origin dev
EOF
put scripts/preflight/fetch-hygiene.test.sh <<'EOF'
git fetch origin dev
EOF
commit_all
run_guard
expect_status 1 "a bare fetch in a sibling test suite still fails"
expect_contains "the sibling suite is named" "scripts/helper.test.sh:1:"

# ---------------------------------------------------------------------------
# 8. Bad usage is a usage error, not a silent pass.
# ---------------------------------------------------------------------------
new_repo usage
commit_all
run_guard --nonsense
expect_status 2 "an unknown argument exits 2"

# ---------------------------------------------------------------------------
# 9. The real tree: the guard's findings must equal its baseline, so CI stays
#    green while #1158's workflow half is blocked -- and goes red the moment a
#    new bare-name fetch is added. (Fix a baselined site without deleting its
#    entry and this fails too, naming the entry; see case 3.)
# ---------------------------------------------------------------------------
out=$(cd "$real_root" && bash "$script" 2>&1)
status=$?
expect_status 0 "the real tree matches its baseline"

# Every baseline entry must name a file that exists: a typo'd path would
# otherwise hide behind `stale` only once the guard runs.
missing=""
while IFS=$'\t' read -r path _command; do
  [[ -z "$path" || "$path" == \#* ]] && continue
  [[ -f "$real_root/$path" ]] || missing+=" $path"
done < "$here/fetch-hygiene.baseline"
out="missing:$missing"
if [[ -z "$missing" ]]; then
  ok "every baseline entry names a tracked file"
else
  no "every baseline entry names a tracked file"
fi

# ---------------------------------------------------------------------------
# 10. Wiring: all.sh must run the guard (the required `ci` job runs all.sh),
#     and the guard must parse.
# ---------------------------------------------------------------------------
if grep -Fq "fetch-hygiene.sh" "$all_sh"; then
  ok "all.sh runs the guard"
else
  no "all.sh runs the guard"
fi
if bash -n "$script"; then
  ok "the guard parses"
else
  no "the guard parses"
fi

if ((failures > 0)); then
  printf '\nFAIL [fetch-hygiene]: %d of %d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf '\nok [fetch-hygiene]: %d checks passed\n' "$checks"
