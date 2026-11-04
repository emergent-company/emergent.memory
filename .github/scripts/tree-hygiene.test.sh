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
# Fixtures quote the forbidden literals verbatim. Backticks and `$` inside the
# single-quoted fixture strings are the test material, not a missing expansion;
# the reviewable form is the literal spelling, so keep them.
# shellcheck disable=SC2016
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
pos mcj-bare       'runner mcj is busy'
pos paseo          'state under ~/.paseo/agents'
pos infra-repo     'see emergent.memory.infra for the runbook'
pos fleet-repo     'see b4ng-link/infrastructure'
pos rig-root       'checkout at /root/emergent/'
pos tailnet-ip     'host is 100.118.32.29'
pos tenant         'tenant legalplant'
pos mac-site       'docs at emergent.memory.mac'
pos old-repo-slug  'cloned emergent-company/emergent yesterday'
pos old-pages-url  'docs at emergent-company.github.io/emergent/latest/'
pos old-repo-eol   'the repo was emergent-company/emergent'
# The clone-URL spellings are the ones this repository actually uses in its
# own docs, and a boundary that only excludes `.` lets both through.
pos old-repo-git   'git clone https://github.com/emergent-company/emergent.git'
pos old-repo-ssh   'git clone git@github.com:emergent-company/emergent.git'
# Private OpenSpec-process pointers (#2443). The slug case is the literal line
# #2249 reintroduced and the issue was filed about; the other two are the forms
# the publication checklist names. The slug alternative has to work in either
# order around the openspec mention.
pos openspec-path     'see openspec/changes/add-documents/proposal.md'
pos openspec-span     'the `openspec` directory must stay private'
pos openspec-slug     '// count and pagination contract (OpenSpec change `documents-list-filters`):'
pos openspec-slug-first '`add-documents-list-filters` is the OpenSpec change'

run_tree "$tree"
expect_status 1 "a tree with leaks fails"
case "$out" in
  *'::error::Internal hostnames'*) ok "the failure is announced for the job log" ;;
  *) no "the failure is announced for the job log" ;;
esac
for name in prose upper colon colon-tight double-space tab-issue tight \
  issue-word tracking-issue memory-hetzner uppercase-host tailnet users-mcj \
  mcj-family mcj-bare paseo infra-repo fleet-repo rig-root tailnet-ip tenant mac-site \
  old-repo-slug old-pages-url old-repo-eol old-repo-git old-repo-ssh \
  openspec-path openspec-span openspec-slug openspec-slug-first; do
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
# `\bmcj\b` must not widen into a longer word (#2297): a surname-like token
# that merely starts with the initials is not the machine name.
write "$tree" mcjones-word.md 'the mcjones machine is not ours'
write "$tree" ssrf-range.md 'the guard rejects 100.64.0.0/10'
write "$tree" docker-ip.md 'bind 10.0.0.5 on the bridge'
# The old-slug alternatives are bounded, so the CURRENT names must stay clean:
# the module path is in every Go import, and the current Pages URL is linked from
# the docs. An unbounded `emergent-company/emergent` would flag the whole tree.
write "$tree" current-module.md 'module github.com/emergent-company/emergent.memory'
write "$tree" current-pages.md 'docs at emergent-company.github.io/emergent.memory/latest/'
# The current module path also appears regex-ESCAPED (every `.` written `\.`) in
# shell and yaml that grep for imports. The boundary has to exclude the backslash
# too, or the gate reds the repo on its own module path.
write "$tree" escaped-module.md 'grep -oh "github\.com/emergent-company/emergent\.memory/[^"]*"'
# A hyphen makes it a different repository, not the old slug.
write "$tree" old-slug-hyphen.md 'see emergent-company/emergent-foo for the fork'
# The OpenSpec alternatives must not flag ordinary prose (#2443): the word
# "specification", a bare openspec mention with no slug, a backticked kebab
# token that is not near an openspec mention, and a backticked CLI flag next to
# one. The slug pattern is anchored to the co-occurrence on purpose -- these are
# the allowed forms the issue names, and a loose "any kebab token" pattern would
# flag all of them.
write "$tree" specification.md 'the specification covers pagination'
write "$tree" bare-openspec.md 'the OpenSpec process is not described here'
write "$tree" slug-without-openspec.md 'run `make-foo-bar` locally'
write "$tree" openspec-flag.md 'the OpenSpec CLI accepts `--dry-run`'
write "$tree" backticked-word.md 'the `specification` is the schema'
run_tree "$tree"
expect_clean "the near misses are not flagged"
status=0

# ---------------------------------------------------------------------------
# 3. Exemptions: the allowlist (with an inline comment), the baseline (a
#    directory prefix with no trailing slash), and the scan's own three files --
#    the workflow, the script and this test, which contain every alternative and
#    would otherwise fail themselves.
# ---------------------------------------------------------------------------
tree=$(fresh exemptions)
write "$tree" docs/ignored.md 'prod runs on memory-hetzner'
write "$tree" docs/other.md 'dev is memory-dev'
write "$tree" vendor/lib/thing.md 'dev is memory-dev'
write "$tree" .github/scripts/tree-hygiene.sh 'memory-hetzner infra#1 \bopenspec/|`openspec`|openspec.*`documents-list-filters`'
write "$tree" .github/workflows/tree-hygiene.yml 'memory-hetzner'
write "$tree" .github/scripts/tree-hygiene.test.sh 'pos prose "see Infra #363"; pos openspec-slug "OpenSpec `documents-list-filters`"'
write "$tree" .allow 'docs/ignored.md   # owned by the preview dispatch
docs/other.md # a second legitimate reference
# memory-hetzner is grandfathered in the baseline, never re-added here'
write "$tree" .base '# prod runs on memory-hetzner (grandfathered)
vendor'
run_tree "$tree" --allowlist .allow --baseline .base
expect_clean "an allowlisted path, a baseline directory, the scan's own three files and the two list files are exempt"
status=0

# An absolute --allowlist/--baseline path still exempts its own file (#2358):
# the comparison used the path as given while the scan reports root-relative
# paths, so the list file itself stayed scannable and a host name in an inline
# comment failed the gate that file configures.
tree=$(fresh absolute-lists)
write "$tree" docs/ignored.md 'nothing to see'
write "$tree" .allow 'docs/ignored.md # memory-hetzner is the prod host'
write "$tree" .base '# memory-dev is the dev host'
run_tree "$tree" --allowlist "$tree/.allow" --baseline "$tree/.base"
expect_clean "an absolute list path exempts the list file itself"
status=0

# A list file named through a symlink whose spelling differs from the scan root
# is still its own exemption (#2370): the comparison stripped "$ROOT/" off the
# spelling as given, so neither `--root <real> --allowlist <link>/.allow` nor the
# reverse matched the list file's scan path -- and a host name in an inline
# comment then failed the gate that file configures.
tree=$(fresh symlinked-spellings)
mkdir -p "$tree/real"
write "$tree" real/.allow 'docs/ignored.md # memory-hetzner is the prod host'
write "$tree" real/docs/ignored.md 'nothing to see'
ln -s "$tree/real" "$tree/link"
status=0
out=$("$script" --root "$tree/real" --allowlist "$tree/link/.allow" --baseline /dev/null 2>&1) || status=$?
expect_clean "a symlinked list path is exempt when --root is the real path"
status=0
out=$("$script" --root "$tree/link" --allowlist "$tree/real/.allow" --baseline /dev/null 2>&1) || status=$?
expect_clean "a real-path list file is exempt when --root is the symlink"
status=0

# Canonicalising the spellings must not turn the gate into a no-op: a real
# violation reached through the symlinked root, and one scanned while the list
# file is reached through the symlink, both still fail and are still named. The
# list file stays exempt in the same run, so the exemption and the failure are
# resolved independently.
tree=$(fresh symlinked-still-fails)
mkdir -p "$tree/real"
write "$tree" real/.allow 'docs/ignored.md # memory-hetzner is the prod host'
write "$tree" real/docs/ignored.md 'nothing to see'
write "$tree" real/leak.md 'prod runs on memory-hetzner'
ln -s "$tree/real" "$tree/link"
status=0
out=$("$script" --root "$tree/link" --allowlist "$tree/real/.allow" --baseline /dev/null 2>&1) || status=$?
expect_status 1 "a violation still fails through a symlinked --root"
if grep -q '^leak\.md:' <<<"$out"; then
  ok "the symlinked-root scan names the offending path"
else
  no "the symlinked-root scan names the offending path"
fi
if grep -q '^\.allow:' <<<"$out"; then
  no "the symlinked-root scan still exempts the list file"
else
  ok "the symlinked-root scan still exempts the list file"
fi
status=0
out=$("$script" --root "$tree/real" --allowlist "$tree/link/.allow" --baseline /dev/null 2>&1) || status=$?
expect_status 1 "a violation still fails when the list file is reached through a symlink"
if grep -q '^leak\.md:' <<<"$out"; then
  ok "the symlinked-list scan names the offending path"
else
  no "the symlinked-list scan names the offending path"
fi
if grep -q '^\.allow:' <<<"$out"; then
  no "the symlinked-list scan still exempts the list file"
else
  ok "the symlinked-list scan still exempts the list file"
fi
status=0

# Those comparisons also have to survive a root that is not written the way the
# scan reports it: `exempt` strips "$ROOT/" off each path, which `--root .` and a
# trailing slash defeat whenever the list is named absolutely (#2358).
tree=$(fresh relative-and-slashy-roots)
write "$tree" .allow '# memory-hetzner is the prod host'
write "$tree" docs/ignored.md 'nothing to see'
status=0
out=$(cd "$tree" && "$script" --root . --allowlist "$tree/.allow" 2>&1) || status=$?
expect_clean "a relative --root leaves an absolute list file exempt"
status=0
out=$("$script" --root "$tree/" --allowlist "$tree/.allow" 2>&1) || status=$?
expect_clean "a trailing slash on --root leaves an absolute list file exempt"
status=0

# A subdirectory root inside a work tree exempts the gate's own files too: the
# scan reports DIR-relative paths while SELF_PATHS are repository-relative, so
# `--root .github/scripts` used to fail on the very scripts that quote the
# forbidden forms on purpose (#2358).
repo=$(fresh subdirectory-root-in-git)
git -C "$repo" init -q
mkdir -p "$repo/.github/scripts"
cp "$script" "$repo/.github/scripts/tree-hygiene.sh"
write "$repo" .github/scripts/other.sh 'nothing to see'
write "$repo" docs/host.md 'prod runs on memory-hetzner'
git -C "$repo" add -A
status=0
out=$("$script" --root "$repo/.github/scripts" 2>&1) || status=$?
expect_clean "a subdirectory root in a work tree exempts the gate's own files"
status=0
status=0
out=$("$script" --root "$repo" 2>&1) || status=$?
expect_status 1 "and the same tree still fails at its root"
if grep -q '^docs/host\.md:' <<<"$out"; then
  ok "the root scan reports the host reference it found"
else
  no "the root scan should report docs/host.md"
fi
status=0

# --self takes either spelling under a subdirectory root (#2358): the usage says
# DIR-relative or repository-relative, and only the DIR-relative form used to work.
status=0
out=$("$script" --root "$repo/docs" --self host.md 2>&1) || status=$?
expect_clean "a DIR-relative --self path is exempt under a subdirectory root"
status=0
out=$("$script" --root "$repo/docs" --self docs/host.md 2>&1) || status=$?
expect_clean "a repository-relative --self path is exempt under a subdirectory root"
status=0

# An allowlist entry written from the repository root still matches under a
# subdirectory root (#2358): scan paths are DIR-relative, so the entry is matched
# in its re-prefixed form too. Without that, a subdirectory scan silently ignored
# every entry in the repository's allowlist.
repo=$(fresh subdirectory-root-allowlist)
git -C "$repo" init -q
mkdir -p "$repo/docs"
write "$repo" .github/tree-hygiene-allowlist.txt 'docs/host.md'
write "$repo" docs/host.md 'prod runs on memory-hetzner'
git -C "$repo" add -A
status=0
out=$("$script" --root "$repo/docs" 2>&1) || status=$?
expect_clean "a repository-relative allowlist entry matches under a subdirectory root"
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

# An absolute --self is exempt the same way an absolute --allowlist is: the
# caller may name the file however it likes (#2358).
tree=$(fresh absolute-self)
write "$tree" notes.md 'prod runs on memory-hetzner'
run_tree "$tree" --self "$tree/notes.md"
expect_clean "an absolute --self path exempts a path"

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
