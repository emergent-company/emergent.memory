#!/usr/bin/env bash
# Hermetic unit tests for ios-release-should-build.sh.
#
# Builds throwaway git repositories in a temp dir (no network) and asserts the
# `changed=...` verdict for the scenarios that caused #1612. Run directly:
#
#   .github/scripts/test-ios-release-should-build.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/ios-release-should-build.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0

# check <label> <expected: true|false> <force: true|false> [ref]
check() {
  local label="$1" want="$2" force="${3:-false}" ref="${4:-}" got
  if [ -n "$ref" ]; then
    git -C "$repo" checkout -q "$ref"
  fi
  got="$(cd "$repo" && FORCE="$force" "$script")"
  if [ "$got" = "changed=$want" ]; then
    echo "ok   - $label ($got)"
    pass=$((pass + 1))
  else
    echo "FAIL - $label: want changed=$want, got '$got'" >&2
    fail=$((fail + 1))
  fi
}

commit() { # commit <message>
  git -C "$repo" add -A
  git -C "$repo" commit -qm "$1"
}

repo="$tmp/repo"
mkdir -p "$repo"
git -C "$repo" init -q -b main
git -C "$repo" config user.email test@example.com
git -C "$repo" config user.name test
mkdir -p "$repo/apps/ios"
echo "app v1" >"$repo/apps/ios/App.swift"
commit "c1"
# Dual tag series on the same commit, as produced by server-sdk.yml.
git -C "$repo" tag v1.0.0
git -C "$repo" tag apps/server/pkg/sdk/v1.0.0

# c2: iOS change, tagged v1.1.0. No marker yet -> bootstrap anchors on v1.0.0.
echo "app v2" >"$repo/apps/ios/App.swift"
commit "c2 ios change"
git -C "$repo" tag v1.1.0
git -C "$repo" tag apps/server/pkg/sdk/v1.1.0
check "bootstrap: iOS change vs previous app tag" true

# Upload of c2 recorded by the build job.
git -C "$repo" tag ios-release-uploaded/v1.1.0

# c3: no iOS change.
echo "docs" >"$repo/README.md"
commit "c3 docs only"
git -C "$repo" tag v1.2.0
git -C "$repo" tag apps/server/pkg/sdk/v1.2.0
check "skip: no iOS change since last upload marker" false
check "skip: re-run of an already-uploaded commit" false false v1.1.0
check "force overrides the skip decision" true true v1.2.0

# c4: iOS change again.
echo "app v3" >"$repo/apps/ios/App.swift"
commit "c4 ios change"
git -C "$repo" tag v1.3.0
git -C "$repo" tag apps/server/pkg/sdk/v1.3.0
check "build: iOS change since last upload marker" true

# c5: iOS change whose tag's build job was skipped (no marker written).
echo "app v4" >"$repo/apps/ios/App.swift"
commit "c5 ios change (build skipped)"
git -C "$repo" tag v1.4.0
git -C "$repo" tag apps/server/pkg/sdk/v1.4.0
# c6: unrelated change; must still see the unconsumed c4+c5 iOS changes.
echo "more docs" >"$repo/README.md"
commit "c6 docs only"
git -C "$repo" tag v1.5.0
git -C "$repo" tag apps/server/pkg/sdk/v1.5.0
check "build: skipped intermediate tag does not consume iOS change" true

# SDK tag series must never be the bootstrap anchor. Build a history where the
# *nearest* ancestor tag of HEAD^ is an SDK module tag: if the fallback used it,
# the c2 iOS change would look "already released" (empty diff) and skip. The
# `v[0-9]*` glob must anchor on the older app tag instead.
repo="$tmp/repo2"
mkdir -p "$repo/apps/ios"
git -C "$repo" init -q -b main
git -C "$repo" config user.email test@example.com
git -C "$repo" config user.name test
echo "app v1" >"$repo/apps/ios/App.swift"
commit "c1"
git -C "$repo" tag v2.0.0
echo "app v2" >"$repo/apps/ios/App.swift"
commit "c2 ios change"
git -C "$repo" tag apps/server/pkg/sdk/v2.1.0 # SDK tag only, no app v2.1.0
echo "noise" >"$repo/noise.txt"
commit "c3 noise"
check "bootstrap: SDK tag series is not the anchor" true false HEAD

echo
echo "passed: $pass, failed: $fail"
[ "$fail" -eq 0 ]
