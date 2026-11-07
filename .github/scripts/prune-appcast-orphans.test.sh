#!/usr/bin/env bash
# Hermetic unit tests for prune-appcast-orphans.sh.
#
# Builds a throwaway gh-pages-shaped fixture in a temp dir (no network, no git)
# and asserts the prune deletes exactly the unreferenced archives. Run directly:
#
#   .github/scripts/prune-appcast-orphans.test.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/prune-appcast-orphans.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0
ok()  { pass=$(( pass + 1 )); echo "ok   - $1"; }
bad() { fail=$(( fail + 1 )); echo "FAIL - $1" >&2; }

# make_file <path> <bytes>: create a file of exactly <bytes> bytes.
make_file() { mkdir -p "$(dirname "$1")"; head -c "$2" /dev/zero > "$1"; }

# write_feed <path> <url...>: feed whose enclosures point at <url...>.
write_feed() {
  local feed="$1"; shift
  mkdir -p "$(dirname "$feed")"
  {
    echo '<?xml version="1.0" standalone="yes"?>'
    echo '<rss version="2.0"><channel>'
    for url in "$@"; do
      echo "  <item><enclosure url=\"${url}\" length=\"1\" type=\"application/octet-stream\"/></item>"
    done
    echo '</channel></rss>'
  } > "$feed"
}

assert_exists() { [ -e "$1" ] && ok "exists: ${1#"$root"/}" || bad "expected to exist: ${1#"$root"/}"; }
assert_absent() { [ ! -e "$1" ] && ok "absent: ${1#"$root"/}" || bad "expected to be gone: ${1#"$root"/}"; }
assert_contains() {
  case "$2" in
    *"$1"*) ok "$3" ;;
    *) bad "$3 (output did not contain '$1'; got: $2)" ;;
  esac
}

# run <args...>: run the script inside $root, capturing stdout+stderr and rc.
run() {
  if out="$(cd "$root" && bash "$script" "$@" 2>&1)"; then rc=0; else rc=$?; fi
}

base="https://emergent-company.github.io/emergent.memory/appcast"

# ---------------------------------------------------------------------------
# Fixture: dev has 2 referenced + 3 orphans (incl. a non-archive binary);
# stable is a second channel that must stay untouched; docs/ and a root file
# are outside appcast/ and must stay untouched.
# ---------------------------------------------------------------------------
root="$tmp/site"
mkdir -p "$root"

write_feed "$root/appcast/dev/appcast.xml" \
  "$base/dev/keep.dmg" \
  "$base/dev/keep.delta?token=x"   # query string must not confuse the match
make_file "$root/appcast/dev/keep.dmg"    100
make_file "$root/appcast/dev/keep.delta"   50
make_file "$root/appcast/dev/orphan.dmg" 1000
make_file "$root/appcast/dev/orphan2.bin" 2000
make_file "$root/appcast/dev/loose.txt"    7

write_feed "$root/appcast/stable/appcast.xml" "$base/stable/s-keep.dmg"
make_file "$root/appcast/stable/s-keep.dmg"     10
make_file "$root/appcast/stable/s-orphan.dmg"  500

make_file "$root/docs/index.html" 9999
make_file "$root/index.html"      1234

# dev orphans: orphan.dmg(1000) + orphan2.bin(2000) + loose.txt(7) = 3007 bytes.
want_total=3007

echo "# dry-run deletes nothing and reports the right total"
run --dry-run dev
assert_contains "prune-total-bytes=${want_total}" "$out" "dry-run total is ${want_total} bytes"
assert_contains "would delete: dev/orphan.dmg (1000 bytes)" "$out" "dry-run lists orphan.dmg with size"
assert_exists "$root/appcast/dev/orphan.dmg"
assert_exists "$root/appcast/dev/orphan2.bin"
assert_exists "$root/appcast/dev/loose.txt"
[ "$rc" -eq 0 ] && ok "dry-run exits 0" || bad "dry-run exited $rc"

echo "# real run deletes unreferenced, keeps referenced and appcast.xml"
run dev
assert_contains "prune-total-bytes=${want_total}" "$out" "real total is ${want_total} bytes"
assert_absent "$root/appcast/dev/orphan.dmg"
assert_absent "$root/appcast/dev/orphan2.bin"
assert_absent "$root/appcast/dev/loose.txt"
assert_exists "$root/appcast/dev/keep.dmg"
assert_exists "$root/appcast/dev/keep.delta"
assert_exists "$root/appcast/dev/appcast.xml"
[ "$rc" -eq 0 ] && ok "real run exits 0" || bad "real run exited $rc"

echo "# the other channel is untouched when pruning one channel"
assert_exists "$root/appcast/stable/s-orphan.dmg"
assert_exists "$root/appcast/stable/s-keep.dmg"
assert_exists "$root/appcast/stable/appcast.xml"

echo "# non-appcast paths are untouched"
assert_exists "$root/docs/index.html"
assert_exists "$root/index.html"

echo "# second run is a no-op"
run dev
assert_contains "prune-total-bytes=0" "$out" "second run reclaims 0 bytes"
[ "$rc" -eq 0 ] && ok "second run exits 0" || bad "second run exited $rc"
assert_exists "$root/appcast/dev/keep.dmg"

echo "# a feed referencing a missing file is reported, not fatal"
write_feed "$root/appcast/edge/appcast.xml" "$base/edge/missing.dmg"
make_file "$root/appcast/edge/other.bin" 321
run edge
[ "$rc" -eq 0 ] && ok "missing-reference run exits 0" || bad "missing-reference run exited $rc"
assert_contains "references missing file missing.dmg" "$out" "missing referenced file is reported"
assert_absent "$root/appcast/edge/other.bin"

echo "# a channel with no appcast.xml is left entirely untouched"
make_file "$root/appcast/broken/keepme.bin" 456
run
assert_exists "$root/appcast/broken/keepme.bin"
assert_exists "$root/appcast/broken" # directory itself stays

echo "# a file outside appcast/ that a feed names is never touched"
# The feed naming it lives under appcast/edge; the file itself is at the root.
make_file "$root/outside.dmg" 88
write_feed "$root/appcast/edge/appcast.xml" "$base/edge/outside.dmg" "$base/edge/missing.dmg"
run edge
assert_exists "$root/outside.dmg"

echo
echo "passed: $pass, failed: $fail"
[ "$fail" -eq 0 ]
