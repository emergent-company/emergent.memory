#!/usr/bin/env bash
# Tests for tree-hygiene.sh — hermetic, runs on the Linux CI runner.
#
# Why this exists (#2343): the scan used to live inside the workflow's `run:`
# body, so the only way to check a pattern change was to retype the regex in a
# shell and hope it was the same expression CI runs. Three regressions shipped
# through that gap -- #2145 (red on main after a version-map change), #2290/#2342
# (a case-sensitive, single-space prefixed-reference pattern let `Infra #363`,
# `INFRA #7`, `infra: #363`, `infra:#364`, `infra  #365` and `infra#366` through)
# and #2336 (ten `infra#621` references merged and survived for hours). Every one
# was found by hand-building a probe file and deleting it afterwards.
#
# Fixtures cannot live in the scanned tree: the gate fails on any tracked file
# containing a fake leak. So each case builds a temporary tree and runs the real
# script over it, which also checks the exemption logic against fixture paths.
set -uo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/tree-hygiene.sh"
workflow="$here/../workflows/tree-hygiene.yml"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

failures=0
ok() { printf 'ok   %s\n' "$1"; }
no() {
  printf 'FAIL %s\n' "$1"
  failures=$((failures + 1))
}

# fresh <name> -- an empty tree for one case, printed
fresh() {
  local d="$tmp/$1"
  rm -rf "$d"
  mkdir -p "$d"
  printf '%s' "$d"
}

# write <tree> <path> <content>
write() {
  mkdir -p "$(dirname "$1/$2")"
  printf '%s\n' "$3" >"$1/$2"
}

out=""
status=0
# run_tree <tree> [args...]
run_tree() {
  local tree=$1
  shift
  status=0
  out=$("$script" --root "$tree" "$@" 2>&1) || status=$?
}

expect_status() {
  if ((status == $1)); then
    ok "$2"
  else
    no "$2 (exit $status, expected $1)"
    printf '%s\n' "$out" | sed 's/^/    /'
  fi
}

expect_clean() {
  if ((status == 0)); then
    ok "$1"
  else
    no "$1 (exit $status)"
    printf '%s\n' "$out" | sed 's/^/    /'
  fi
}

# ---------------------------------------------------------------------------
# 1. Every prefixed-reference form is caught. The nine cases are the incident
#    from #2290/#2342/#2336; a pattern that is case-sensitive, single-space, or
#    needs the space before `#` fails exactly one of them.
# ---------------------------------------------------------------------------
tree=$(fresh caught)
pos() { write "$tree" "$1.md" "$2"; }
pos prose          'see Infra #363 for the host change'
pos upper          'see INFRA #7'
pos colon          'see infra: #363'
pos colon-tight    'see infra:#364'
pos double-space   'see infra  #365'
pos tab-issue      $'see infra\tissue\t#123'
pos tight          'see infra#366'
pos issue-word     'see infra issue #123'
pos tracking-issue 'see infra tracking issue #321'
pos memory-hetzner 'prod runs on memory-hetzner'
pos uppercase-host 'prod runs on MEMORY-HETZNER'
pos tailnet        'dig rig.example.ts.net'
pos users-mcj      'path /Users/mcj/Library/Caches'
pos mcj-family     'runner mcj-mini is busy'
pos paseo          'state under ~/.paseo/agents'
pos infra-repo     'see emergent.memory.infra for the runbook'
pos fleet-repo     'see b4ng-link/infrastructure'
pos rig-root       'checkout at /root/emergent/'
pos tailnet-ip     'host is 100.118.32.29'
pos tenant         'tenant legalplant'
pos mac-site       'docs at emergent.memory.mac'

run_tree "$tree"
expect_status 1 "a tree with leaks fails"
case "$out" in
  *'::error::Internal hostnames'*) ok "the failure is announced for the job log" ;;
  *) no "the failure is announced for the job log" ;;
esac
for name in prose upper colon colon-tight double-space tab-issue tight \
  issue-word tracking-issue memory-hetzner uppercase-host tailnet users-mcj \
  mcj-family paseo infra-repo fleet-repo rig-root tailnet-ip tenant mac-site; do
  if grep -q "^${name}\.md:" <<<"$out"; then
    ok "caught: $name"
  else
    no "caught: $name"
  fi
done

# ---------------------------------------------------------------------------
# 2. The near misses stay clean. Over-matching blocks a legitimate sentence just
#    as hard as a leak, and the two directions are what make the pattern
#    reviewable.
# ---------------------------------------------------------------------------
tree=$(fresh near-misses)
write "$tree" infrastructure.md 'Infrastructure #3 is a product issue'
write "$tree" bare-number.md 'see #370'
write "$tree" infra-dash.md 'see infra-foo #371'
write "$tree" plain-infra.md 'the infra team owns this service'
write "$tree" ssrf-range.md 'the guard rejects 100.64.0.0/10'
write "$tree" docker-ip.md 'bind 10.0.0.5 on the bridge'
run_tree "$tree"
expect_clean "the near misses are not flagged"
status=0

# ---------------------------------------------------------------------------
# 3. Exemptions: the allowlist (with an inline comment), the baseline (a
#    directory prefix with no trailing slash), and the scan's own two files --
#    which contain every alternative and would otherwise fail themselves.
# ---------------------------------------------------------------------------
tree=$(fresh exemptions)
write "$tree" docs/ignored.md 'prod runs on memory-hetzner'
write "$tree" docs/other.md 'dev is memory-dev'
write "$tree" vendor/lib/thing.md 'dev is memory-dev'
write "$tree" .github/scripts/tree-hygiene.sh 'memory-hetzner infra#1'
write "$tree" .github/workflows/tree-hygiene.yml 'memory-hetzner'
write "$tree" .github/scripts/tree-hygiene.test.sh 'pos prose "see Infra #363"'
write "$tree" .allow 'docs/ignored.md   # owned by the preview dispatch
docs/other.md # a second legitimate reference
# memory-hetzner is grandfathered in the baseline, never re-added here'
write "$tree" .base '# prod runs on memory-hetzner (grandfathered)
vendor'
run_tree "$tree" --allowlist .allow --baseline .base
expect_clean "an allowlisted path, a baseline directory, the scan's own three files and the two list files are exempt"
status=0

# ---------------------------------------------------------------------------
# 4. ... and the exemption is exact. A file next to an allowlisted one, whose
#    name merely starts with it, is still a violation: prefix matching here
#    would silently widen every allowlist entry.
# ---------------------------------------------------------------------------
tree=$(fresh prefix-collision)
write "$tree" docs/ignored.md 'prod runs on memory-hetzner'
write "$tree" docs/ignored.md.bak 'prod runs on memory-hetzner'
write "$tree" .allow 'docs/ignored.md'
run_tree "$tree" --allowlist .allow --baseline /dev/null
expect_status 1 "a path that only starts with an allowlisted name is not exempt"
if grep -q '^docs/ignored\.md\.bak:' <<<"$out"; then
  ok "the collision case names the file outside the exemption"
else
  no "the collision case names the file outside the exemption"
fi
if grep -q '^docs/ignored\.md:' <<<"$out"; then
  no "the allowlisted file itself stays exempt"
else
  ok "the allowlisted file itself stays exempt"
fi

# A one-file tree is the fixture shape, and grep drops the path prefix when only
# one file matches: the report and the exemption both split on that prefix, so
# this pins it explicitly (the script passes -H for exactly this reason).
tree=$(fresh single-file)
write "$tree" only.md 'prod runs on memory-hetzner'
run_tree "$tree"
expect_status 1 "a single matching file still reports its path"
if grep -q '^only\.md:' <<<"$out"; then
  ok "the one-file report is path:line:text, not line:text"
else
  no "the one-file report is path:line:text, not line:text"
fi

# ---------------------------------------------------------------------------
# 5. `--self` adds an exemption (the option exists so callers can extend the
#    always-exempt set without editing the script).
# ---------------------------------------------------------------------------
tree=$(fresh self)
write "$tree" notes.md 'prod runs on memory-hetzner'
run_tree "$tree" --self notes.md
expect_clean "--self exempts a path"

# ---------------------------------------------------------------------------
# 6. The gate is wired to the script, and the workflow no longer carries a
#    second copy of the scan. A detach here would restore exactly the
#    untested-gate state this test exists to prevent.
# ---------------------------------------------------------------------------
# The `run:` line itself, not any mention of the path: the step's comment names
# it too, so a string match would pass a step that was disabled with a comment
# left behind (found in review of #2356).
if grep -qE '^[[:space:]]*run:[[:space:]]*\.github/scripts/tree-hygiene\.sh[[:space:]]*$' "$workflow"; then
  ok "the workflow's run: line executes the tested script"
else
  no "the workflow's run: line executes the tested script"
fi
if grep -q 'git grep -n -I -i -E' "$workflow"; then
  no "the workflow carries its own scan again (the script is the single source)"
else
  ok "the workflow carries no scan of its own"
fi

# ---------------------------------------------------------------------------
# 7. Usage errors fail closed instead of scanning nothing and reporting OK.
# ---------------------------------------------------------------------------
out=$("$script" --nope 2>&1)
status=$?
expect_status 2 "an unknown option is a usage error"
out=$("$script" --root "$tmp/does-not-exist" 2>&1)
status=$?
expect_status 2 "a missing root is a usage error, not a clean tree"
status=0

if ((failures)); then
  printf 'tree-hygiene.test: FAILED (%d)\n' "$failures" >&2
  exit 1
fi
echo "tree-hygiene.test: passed"
