#!/usr/bin/env bash
# Hermetic unit tests for mac-release-should-build.sh.
#
# Builds throwaway git repositories in a temp dir (no network) and asserts the
# `changed=...` verdict for the scenarios that caused the Mac build to be
# silently skipped despite app changes. Run directly:
#
#   .github/scripts/mac-release-should-build.test.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/mac-release-should-build.sh"
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
mkdir -p "$repo/apps/connector.mac"
echo "app v1" >"$repo/apps/connector.mac/App.swift"
commit "c1"
# Dual tag series on the same commit, as produced by server-sdk.yml.
git -C "$repo" tag v1.0.0
git -C "$repo" tag apps/server/pkg/sdk/v1.0.0

# c2: Mac app change, tagged v1.1.0. No marker yet -> bootstrap on v1.0.0.
echo "app v2" >"$repo/apps/connector.mac/App.swift"
commit "c2 mac change"
git -C "$repo" tag v1.1.0
git -C "$repo" tag apps/server/pkg/sdk/v1.1.0
check "bootstrap: mac change vs previous app tag" true

# Build of c2 recorded by the build job.
git -C "$repo" tag mac-release-uploaded/v1.1.0

# c3: no Mac app change.
echo "docs" >"$repo/README.md"
commit "c3 docs only"
git -C "$repo" tag v1.2.0
git -C "$repo" tag apps/server/pkg/sdk/v1.2.0
check "skip: no mac change since last build marker" false
check "skip: re-run of an already-built commit" false false v1.1.0
check "force overrides the skip decision" true true v1.2.0

# c4: Mac app change again.
echo "app v3" >"$repo/apps/connector.mac/App.swift"
commit "c4 mac change"
git -C "$repo" tag v1.3.0
git -C "$repo" tag apps/server/pkg/sdk/v1.3.0
check "build: mac change since last build marker" true

# c5: mac change whose tag's build job was skipped (no marker written).
echo "app v4" >"$repo/apps/connector.mac/App.swift"
commit "c5 mac change (build skipped)"
git -C "$repo" tag v1.4.0
git -C "$repo" tag apps/server/pkg/sdk/v1.4.0
# c6: unrelated change; must still see the unconsumed c4+c5 mac changes.
echo "more docs" >"$repo/README.md"
commit "c6 docs only"
git -C "$repo" tag v1.5.0
git -C "$repo" tag apps/server/pkg/sdk/v1.5.0
check "build: skipped intermediate tag does not consume mac change" true

# SDK tag series must never be the bootstrap anchor. Build a history where the
# *nearest* ancestor tag of HEAD^ is an SDK module tag: if the fallback used it,
# the c2 mac change would look "already released" (empty diff) and skip. The
# `v[0-9]*` glob must anchor on the older app tag instead.
repo="$tmp/repo2"
mkdir -p "$repo/apps/connector.mac"
git -C "$repo" init -q -b main
git -C "$repo" config user.email test@example.com
git -C "$repo" config user.name test
echo "app v1" >"$repo/apps/connector.mac/App.swift"
commit "c1"
git -C "$repo" tag v2.0.0
echo "app v2" >"$repo/apps/connector.mac/App.swift"
commit "c2 mac change"
git -C "$repo" tag apps/server/pkg/sdk/v2.1.0 # SDK tag only, no app v2.1.0
echo "noise" >"$repo/noise.txt"
commit "c3 noise"
check "bootstrap: SDK tag series is not the anchor" true false HEAD

echo
echo "passed: $pass, failed: $fail"
[ "$fail" -eq 0 ]
