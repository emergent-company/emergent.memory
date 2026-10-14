#!/usr/bin/env bash
# Hermetic regression test for openspec-archive.sh (#1858).
#
# The guard must enumerate candidates from the BASE ref, not from the working
# tree. When it read `openspec list` (working tree) a change that had been
# merged to the base but was absent from a stale/dirty local checkout was never
# examined, so the guard printed a false OK. This test builds a throwaway repo,
# replays that exact shape, and asserts drift is still found.
#
# Pure git + python3; no openspec CLI, no network. Run: bash scripts/preflight/openspec-archive_test.sh
set -euo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/openspec-archive.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

cd "$tmp"
git init -q .
git config user.email test@example.com
git config user.name test

# commit 0: no changes at all.
git commit -q --allow-empty -m "init"
c0="$(git rev-parse HEAD)"

# commit 1: one archive-ready change (all tasks ticked) and one held change.
mkdir -p openspec/changes/ready-change openspec/changes/held-change
cat > openspec/changes/ready-change/tasks.md <<'EOF'
# ready-change

- [x] 1.1 do the thing
- [x] 1.2 test the thing
EOF
cat > openspec/changes/held-change/tasks.md <<'EOF'
# held-change

- [ ] 1.1 deliberately deferred
<!-- openspec:archive-hold: shipped partial, rest deferred -->
EOF
git add -A
git commit -q -m "add changes"
c1="$(git rev-parse HEAD)"

echo "== case 1: stale tree, base has an archive-ready change (the #1858 bug) =="
git checkout -q "$c0"
out="$(bash "$script" --base "$c1" 2>&1 || true)"
rc=0; bash "$script" --base "$c1" >/dev/null 2>&1 || rc=$?
[ "$rc" -eq 1 ] || fail "expected exit 1 on stale tree, got $rc"
echo "$out" | grep -q "ready-change (2/2 tasks complete)" \
  || fail "ready change not flagged from base ref"
echo "$out" | grep -q "held-change" || fail "held change not reported"
echo "$out" | grep -q "shipped partial, rest deferred" || fail "hold reason not reported"
grep -q 'openspec-archive: OK' <<<"$out" && fail "stale tree printed a false OK"
echo "ok"

echo "== case 2: fresh tree agrees with the stale tree (enumeration is tree-independent) =="
git checkout -q "$c1"
fresh="$(bash "$script" --base "$c1" 2>&1 || true)"
[ "$fresh" = "$out" ] || fail "fresh and stale trees disagreed"
echo "ok"

echo "== case 3: base without the change is clean =="
rc=0; bash "$script" --base "$c0" >/dev/null 2>&1 || rc=$?
[ "$rc" -eq 0 ] || fail "expected exit 0 when the change is absent from base, got $rc"
echo "ok"

echo "== case 4: dirty working tree cannot hide drift =="
git checkout -q "$c1"
rm -rf openspec/changes/ready-change
rc=0; bash "$script" --base "$c1" >/dev/null 2>&1 || rc=$?
[ "$rc" -eq 1 ] || fail "expected exit 1 with a deleted local change dir, got $rc"
git checkout -q -- .
echo "ok"

echo "== case 5: --json shape =="
count="$(bash "$script" --base "$c1" --json | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["count"], d["heldCount"])' || true)"
[ "$count" = "1 1" ] || fail "unexpected json counts: $count"
echo "ok"

echo "openspec-archive_test: OK (5 cases)"
