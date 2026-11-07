#!/usr/bin/env bash
# Hermetic unit tests for check-pages-tree-size.sh.
#
# Builds throwaway directory trees in a temp dir (no network) with known byte
# totals and asserts the aggregate guard passes/fails and reports the real total.
# Run directly:
#
#   .github/scripts/check-pages-tree-size.test.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/check-pages-tree-size.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0
ok()  { pass=$(( pass + 1 )); echo "ok   - $1"; }
bad() { fail=$(( fail + 1 )); echo "FAIL - $1" >&2; }
assert_contains() {
  case "$2" in
    *"$1"*) ok "$3" ;;
    *) bad "$3 (output did not contain '$1'; got: $2)" ;;
  esac
}

make_file() { mkdir -p "$(dirname "$1")"; head -c "$2" /dev/zero > "$1"; }

# run <env-limit-or-empty> <args...>: run the guard, capturing output and rc.
run() {
  local limit="$1"; shift
  if [ -n "$limit" ]; then
    if out="$(PAGES_TREE_LIMIT_MIB="$limit" bash "$script" "$@" 2>&1)"; then rc=0; else rc=$?; fi
  else
    if out="$(bash "$script" "$@" 2>&1)"; then rc=0; else rc=$?; fi
  fi
}

# ---------------------------------------------------------------------------
# 2 MiB of real content + a 5 MiB .git blob that must NOT be counted.
# ---------------------------------------------------------------------------
root="$tmp/site"
mkdir -p "$root/appcast"
make_file "$root/index.html"        1048576   # 1 MiB
make_file "$root/appcast/a.dmg"    1048576   # 1 MiB
make_file "$root/.git/objects/big" 5242880   # 5 MiB, excluded
total=2097152

echo "# passes under the default 500 MiB guard and echoes the real total"
run "" "$root"
[ "$rc" -eq 0 ] && ok "default guard passes" || bad "default guard failed (rc=$rc)"
assert_contains "${total} bytes" "$out" "echoes the exact byte total (${total})"
assert_contains "2 MiB" "$out" "echoes the MiB total"
assert_contains "2 file(s)" "$out" "counts only the 2 published files (.git excluded)"

echo "# the .git directory is excluded (5 MiB blob would otherwise blow a 2 MiB guard)"
run 2 "$root"
[ "$rc" -eq 0 ] && ok "5 MiB .git blob is not counted" || bad ".git blob leaked into the total (rc=$rc)"

echo "# fails when the total is over the configured limit"
run 1 "$root"
[ "$rc" -eq 1 ] && ok "over-limit tree exits 1" || bad "over-limit tree exited $rc, want 1"
assert_contains "${total} bytes" "$out" "failure still echoes the actual total"

echo "# honours PAGES_TREE_LIMIT_MIB"
run 5 "$root"
[ "$rc" -eq 0 ] && ok "5 MiB limit passes a 2 MiB tree" || bad "5 MiB limit rejected a 2 MiB tree (rc=$rc)"

echo "# boundary: exactly at the limit passes, one byte over fails"
# 2 MiB limit == 2097152 bytes; the tree is exactly that.
run 2 "$root"
[ "$rc" -eq 0 ] && ok "total == limit passes" || bad "total == limit was rejected (rc=$rc)"
make_file "$root/onebyte" 1
run 2 "$root"
[ "$rc" -eq 1 ] && ok "one byte over the limit fails" || bad "one byte over the limit passed (rc=$rc)"
assert_contains "2097153 bytes" "$out" "echoes the bumped total"

echo "# accepts an explicit file list as well as a directory"
run "" "$root/index.html" "$root/appcast/a.dmg"
[ "$rc" -eq 0 ] && ok "file-list mode passes" || bad "file-list mode failed (rc=$rc)"
assert_contains "${total} bytes" "$out" "file-list mode sums the given files"

echo "# a missing path is an error, not a silent zero"
if out="$(bash "$script" "$tmp/does-not-exist" 2>&1)"; then rc=0; else rc=$?; fi
[ "$rc" -eq 1 ] && ok "missing path exits 1" || bad "missing path exited $rc, want 1"

echo
echo "passed: $pass, failed: $fail"
[ "$fail" -eq 0 ]
