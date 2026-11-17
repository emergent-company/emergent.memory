#!/usr/bin/env bash
# Hermetic tests for prune-doc-versions.sh.
#
# Builds throwaway gh-pages-shaped trees in a temp dir (no network, no git, no
# mike) and asserts the retention window deletes exactly the superseded version
# trees, rewrites versions.json to match, and leaves everything else -- the
# appcast, the alias link, the root-level files -- alone. The cases also pin the
# fail-closed behaviour, which is what protects the docs site's version selector
# from a bad rewrite that would 404 every version. Run directly:
#
#   .github/scripts/prune-doc-versions.test.sh
set -uo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/prune-doc-versions.sh"
publish_script="$here/publish-appcast.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0
ok()  { pass=$(( pass + 1 )); echo "ok   - $1"; }
bad() { fail=$(( fail + 1 )); echo "FAIL - $1" >&2; }

# make_file <path> <bytes>: create a file of exactly <bytes> bytes.
make_file() { mkdir -p "$(dirname "$1")"; head -c "$2" /dev/zero > "$1"; }

# fixture <root> <version>[:<alias>]...: a gh-pages-shaped tree. Every entry gets
# a directory of <PAGE_BYTES> bytes, aliases become symlinks like mike's, and the
# paths that must never be touched (appcast/, root files, .nojekyll) exist too.
PAGE_BYTES=1000
fixture() {
  local root="$1"; shift
  local entry version alias aliases first=1
  rm -rf "$root"
  mkdir -p "$root"
  printf '[' > "$root/versions.json"
  for entry in "$@"; do
    version="${entry%%:*}"
    aliases=""
    if [ "$version" != "$entry" ]; then
      for alias in $(echo "${entry#*:}" | tr ',' ' '); do
        aliases+="${aliases:+, }\"$alias\""
        ln -sfn "$version" "$root/$alias"
      done
    fi
    [ "$first" -eq 1 ] || printf ',' >> "$root/versions.json"
    first=0
    printf '\n  {\n    "version": "%s",\n    "title": "%s",\n    "aliases": [%s]\n  }' \
      "$version" "$version" "$aliases" >> "$root/versions.json"
    make_file "$root/$version/index.html" "$PAGE_BYTES"
  done
  printf '\n]\n' >> "$root/versions.json"
  make_file "$root/appcast/stable/appcast.xml" 10
  make_file "$root/appcast/stable/Memory-1.0.0.dmg" 5000
  make_file "$root/index.html" 20
  make_file "$root/.nojekyll" 0
  make_file "$root/swagger.json" 30
}

# entries <root>: the version names versions.json still lists, in file order.
entries() {
  python3 -c 'import json,sys
d=json.load(open(sys.argv[1]))
if isinstance(d,dict): d=d.get("versions",[])
print("\n".join(e["version"] for e in d))' "$1/versions.json"
}
entry_count() { entries "$1" | grep -c . ; }

# run <root> <args...>: run the script against <root>, capturing output and rc.
run() {
  local root="$1"; shift
  if out="$(bash "$script" --root "$root" "$@" 2>&1)"; then rc=0; else rc=$?; fi
}
assert_rc()      { [ "$rc" -eq "$1" ] && ok "$2" || bad "$2 (exit $rc, want $1; output: $out)"; }
assert_exists()  { [ -e "$1" ] && ok "exists: ${1#"$tmp"/}" || bad "expected to exist: ${1#"$tmp"/}"; }
assert_absent()  { [ ! -e "$1" ] && ok "absent: ${1#"$tmp"/}" || bad "expected to be gone: ${1#"$tmp"/}"; }
assert_contains() {
  case "$2" in
    *"$1"*) ok "$3" ;;
    *) bad "$3 (output did not contain '$1'; got: $2)" ;;
  esac
}
assert_entries() {
  # assert_entries <root> <description> <expected versions...>
  local root="$1" desc="$2"; shift 2
  local want got
  want="$(printf '%s\n' "$@" | sed '/^$/d' | sort)"
  got="$(entries "$root" | sed '/^$/d' | sort)"
  [ "$want" = "$got" ] && ok "$desc" || bad "$desc (entries: [$(echo "$got" | tr '\n' ' ')] want: [$(echo "$want" | tr '\n' ' ')])"
}

# ---------------------------------------------------------------------------
# 1. The reported shape: versioned trees plus the rolling `dev` tree and a
#    `latest` symlink. Keeping the newest 3 drops the 3 oldest and nothing else.
# ---------------------------------------------------------------------------
echo "# prunes the oldest versioned trees and keeps the newest N plus dev"
root="$tmp/site"
fixture "$root" dev v0.101.0:latest v0.100.0 v0.99.0 v0.98.0 v0.97.0 v0.96.0
run "$root" --keep 3
assert_rc 0 "default-shaped tree exits 0"
assert_absent "$root/v0.96.0"
assert_absent "$root/v0.97.0"
assert_absent "$root/v0.98.0"
assert_exists "$root/v0.99.0/index.html"
assert_exists "$root/v0.100.0/index.html"
assert_exists "$root/v0.101.0/index.html"
assert_exists "$root/dev/index.html"
assert_exists "$root/latest"
[ "$(readlink "$root/latest")" = "v0.101.0" ] && ok "the latest alias still points at the newest tree" \
  || bad "latest alias changed to $(readlink "$root/latest" 2>/dev/null)"
assert_entries "$root" "versions.json lists only the kept trees" dev v0.101.0 v0.100.0 v0.99.0
assert_contains "deleted: v0.96.0" "$out" "names each deleted tree"
assert_contains "reclaimed $(( PAGE_BYTES * 3 )) bytes" "$out" "reports the reclaimed bytes"
strays="$(find "$root" -maxdepth 1 -name 'versions.json.*' -print)"
[ -z "$strays" ] && ok "leaves no temp file behind in the tree" || bad "left a temp file: ${strays}"

echo "# the appcast and the root-level files are untouched"
assert_exists "$root/appcast/stable/appcast.xml"
assert_exists "$root/appcast/stable/Memory-1.0.0.dmg"
assert_exists "$root/index.html"
assert_exists "$root/swagger.json"
assert_exists "$root/.nojekyll"

# ---------------------------------------------------------------------------
# 2. Ordering is numeric, not lexicographic: v0.100.0 is newer than v0.99.0 and
#    v0.89.0 newer than v0.88.10. A string sort keeps the wrong two.
# ---------------------------------------------------------------------------
echo "# orders versions numerically"
root="$tmp/numeric"
fixture "$root" v0.88.10 v0.89.0 v0.99.0 v0.100.0
run "$root" --keep 2
assert_rc 0 "numeric case exits 0"
assert_absent "$root/v0.88.10"
assert_absent "$root/v0.89.0"
assert_exists "$root/v0.99.0/index.html"
assert_exists "$root/v0.100.0/index.html"
assert_entries "$root" "keeps v0.100.0 and v0.99.0" v0.99.0 v0.100.0

# ---------------------------------------------------------------------------
# 3. An aliased version is never pruned, even far outside the window: deleting
#    it would leave the alias (and every unversioned link into the site) 404ing.
# ---------------------------------------------------------------------------
echo "# an aliased old version is never pruned"
root="$tmp/aliased"
fixture "$root" v0.50.0:pinned v0.101.0:latest v0.100.0 v0.99.0
run "$root" --keep 2
assert_rc 0 "aliased case exits 0"
assert_exists "$root/v0.50.0/index.html"
assert_exists "$root/pinned"
assert_absent "$root/v0.99.0"
assert_entries "$root" "keeps v0.50.0 because it carries an alias" v0.50.0 v0.101.0 v0.100.0

# ---------------------------------------------------------------------------
# 4. Nothing to prune is a no-op that leaves versions.json byte-identical: the
#    prune runs on every release publish, and a rewrite that reordered or
#    reformatted the file for no reason would be a needless diff.
# ---------------------------------------------------------------------------
echo "# nothing to prune is a no-op"
root="$tmp/noop"
fixture "$root" dev v0.101.0:latest v0.100.0
cp "$root/versions.json" "$tmp/noop.expected"
run "$root"
assert_rc 0 "under the window exits 0"
assert_contains "nothing to prune" "$out" "says there was nothing to prune"
cmp -s "$root/versions.json" "$tmp/noop.expected" && ok "versions.json is unchanged" || bad "versions.json was rewritten"

echo "# a second run after pruning is a no-op"
root="$tmp/idempotent"
fixture "$root" v0.101.0:latest v0.100.0 v0.99.0
run "$root" --keep 2
cp "$root/versions.json" "$tmp/idempotent.expected"
run "$root" --keep 2
assert_rc 0 "second run exits 0"
assert_contains "nothing to prune" "$out" "second run prunes nothing"
cmp -s "$root/versions.json" "$tmp/idempotent.expected" && ok "second run left versions.json alone" || bad "second run rewrote versions.json"

# ---------------------------------------------------------------------------
# 5. --dry-run prints the plan and changes nothing.
# ---------------------------------------------------------------------------
echo "# --dry-run deletes nothing"
root="$tmp/dry"
fixture "$root" v0.101.0:latest v0.100.0 v0.99.0
cp "$root/versions.json" "$tmp/dry.expected"
run "$root" --dry-run --keep 1
assert_rc 0 "dry run exits 0"
assert_exists "$root/v0.99.0/index.html"
cmp -s "$root/versions.json" "$tmp/dry.expected" && ok "dry run left versions.json alone" || bad "dry run rewrote versions.json"
assert_contains "would delete: v0.99.0" "$out" "dry run names a tree it would prune"
assert_contains "dry run" "$out" "dry run is announced"

# ---------------------------------------------------------------------------
# 6. DOC_VERSIONS_TO_KEEP is the documented override; --keep wins over it.
# ---------------------------------------------------------------------------
echo "# DOC_VERSIONS_TO_KEEP overrides the default"
root="$tmp/env"
fixture "$root" v0.101.0:latest v0.100.0 v0.99.0 v0.98.0
if out="$(DOC_VERSIONS_TO_KEEP=2 bash "$script" --root "$root" 2>&1)"; then rc=0; else rc=$?; fi
assert_rc 0 "env override exits 0"
assert_entries "$root" "env override keeps 2" v0.101.0 v0.100.0

echo "# --keep wins over DOC_VERSIONS_TO_KEEP"
root="$tmp/env-precedence"
fixture "$root" v0.101.0:latest v0.100.0 v0.99.0 v0.98.0
if out="$(DOC_VERSIONS_TO_KEEP=2 bash "$script" --root "$root" --keep 1 2>&1)"; then rc=0; else rc=$?; fi
assert_rc 0 "--keep overrides env"
assert_entries "$root" "explicit --keep 1 keeps only the newest" v0.101.0

# ---------------------------------------------------------------------------
# 7. A stale entry (versions.json names a tree that is not on disk) is dropped
#    rather than left to 404 -- the same defect, from the other direction.
# ---------------------------------------------------------------------------
echo "# an entry with no directory is dropped, not an error"
root="$tmp/stale"
fixture "$root" v0.101.0:latest v0.100.0
python3 - "$root/versions.json" <<'PY'
import json, sys
path = sys.argv[1]
entries = json.load(open(path))
entries.append({"version": "v0.1.0", "title": "v0.1.0", "aliases": []})
json.dump(entries, open(path, "w"), indent=2)
open(path, "a").write("\n")
PY
run "$root" --keep 1
assert_rc 0 "stale entry exits 0"
assert_contains "removed stale entry: v0.1.0" "$out" "reports the stale entry"
assert_entries "$root" "drops the stale entry" v0.101.0

# ---------------------------------------------------------------------------
# 8. A minified versions.json (single line) is rewritten correctly: the file is
#    parsed, never string-surgery'd, so formatting cannot change the outcome.
# ---------------------------------------------------------------------------
echo "# a minified versions.json is handled"
root="$tmp/minified"
fixture "$root" v0.101.0:latest v0.100.0 v0.99.0
python3 - "$root/versions.json" <<'PY'
import json, sys
path = sys.argv[1]
json.dump(json.load(open(path)), open(path, "w"))  # no indent: one line
PY
run "$root" --keep 1
assert_rc 0 "minified case exits 0"
assert_entries "$root" "minified rewrite keeps only the newest" v0.101.0

# ---------------------------------------------------------------------------
# 9. Fail closed. Every one of these must leave the tree exactly as it was: a
#    half-applied prune breaks the version selector for every version, not just
#    the ones it meant to drop.
# ---------------------------------------------------------------------------
echo "# a usage error exits 2 and changes nothing"
root="$tmp/usage"
fixture "$root" v0.101.0:latest v0.100.0
cp "$root/versions.json" "$tmp/usage.expected"
run "$root" --keep 0
assert_rc 2 "--keep 0 is a usage error"
run "$root" --keep abc
assert_rc 2 "--keep abc is a usage error"
run "$root" --nope
assert_rc 2 "an unknown option is a usage error"
if out="$(bash "$script" --root "$tmp/does-not-exist" 2>&1)"; then rc=0; else rc=$?; fi
[ "$rc" -eq 1 ] && ok "a missing root exits 1" || bad "a missing root exited $rc, want 1"
assert_exists "$root/v0.100.0/index.html"
cmp -s "$root/versions.json" "$tmp/usage.expected" && ok "usage errors left versions.json alone" || bad "usage errors rewrote versions.json"

echo "# an unreadable versions.json fails the run and deletes nothing"
root="$tmp/malformed"
fixture "$root" v0.101.0:latest v0.100.0 v0.99.0
printf '{not json\n' > "$root/versions.json"
run "$root" --keep 1
[ "$rc" -ne 0 ] && ok "malformed versions.json fails the run" || bad "malformed versions.json exited 0"
assert_exists "$root/v0.99.0/index.html"
assert_exists "$root/v0.100.0/index.html"

echo "# a versions.json that is not a list fails the run and deletes nothing"
root="$tmp/notalist"
fixture "$root" v0.101.0:latest v0.100.0 v0.99.0
printf '"a string"\n' > "$root/versions.json"
run "$root" --keep 1
[ "$rc" -ne 0 ] && ok "non-list versions.json fails the run" || bad "non-list versions.json exited 0"
assert_exists "$root/v0.99.0/index.html"

echo "# a tree with no versions.json is a no-op"
root="$tmp/none"
mkdir -p "$root"
make_file "$root/index.html" 20
run "$root"
assert_rc 0 "no versions.json exits 0"
assert_contains "nothing to do" "$out" "says there is nothing to do"
assert_exists "$root/index.html"

# ---------------------------------------------------------------------------
# 10. The wiring: publish-appcast.sh prunes before the aggregate guard measures
#     the tree, and the guard runs before the commit. A prune placed after the
#     guard would leave the release red on exactly the tree it was meant to fix.
# ---------------------------------------------------------------------------
echo "# publish-appcast.sh prunes superseded docs trees before the aggregate guard"
prune_line="$(grep -nE '^[[:space:]]*bash "\$\{script_dir\}/prune-doc-versions\.sh"' "$publish_script" | head -n 1 | cut -d: -f1)"
guard_line="$(grep -n 'check-pages-tree-size\.sh" \.$' "$publish_script" | head -n 1 | cut -d: -f1)"
if [ -n "$prune_line" ] && [ -n "$guard_line" ] && [ "$prune_line" -lt "$guard_line" ]; then
  ok "publish-appcast.sh prunes (line ${prune_line}) before the tree guard (line ${guard_line})"
else
  bad "publish-appcast.sh does not prune before the tree guard (prune=${prune_line:-none} guard=${guard_line:-none})"
fi

# The preflight runner is what makes these tests run in the required `ci`
# context (ci.yml already calls scripts/preflight/all.sh); a check that drops off
# its list stops running silently.
preflight_list="$(dirname "$here")/../scripts/preflight/all.sh"
if [ -f "$preflight_list" ] && grep -q 'gh-pages-budget\.sh' "$preflight_list"; then
  ok "scripts/preflight/all.sh runs the gh-pages budget self-tests"
else
  bad "scripts/preflight/all.sh does not run the gh-pages budget self-tests"
fi

echo
echo "passed: $pass, failed: $fail"
[ "$fail" -eq 0 ]
