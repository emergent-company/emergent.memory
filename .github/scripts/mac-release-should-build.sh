#!/usr/bin/env bash
# Decide whether the Mac Release workflow should build + publish the DMG.
#
# Prints the GitHub Actions step output `changed=true|false`.
#
# Mirrors ios-release-should-build.sh (#1612). Anchor (preferred): the newest
# `mac-release-uploaded/vX.Y.Z` marker tag reachable from HEAD, written by the
# build job only after a successful build. This is resilient to:
#   * the dual tag series — both `vX.Y.Z` and `apps/server/pkg/sdk/vX.Y.Z` are
#     pushed for every bump, and `git describe` picks between them
#     nondeterministically; markers are independent of that series, and
#   * intermediate releases whose build job was skipped — a change is never
#     "consumed" by a tag that never built, because the next tag still diffs
#     against the last *built* commit.
#
# Bootstrap: before any marker exists, fall back to the previous app-release tag
# (`vX.Y.Z`). The glob is anchored at `v`, so the Go module tags
# (`apps/server/pkg/sdk/vX.Y.Z`) can never become the anchor.
set -euo pipefail

apps_dir="${MAC_APPS_DIR:-apps/connector.mac}"
marker_glob="${MAC_MARKER_GLOB:-mac-release-uploaded/*}"
app_tag_glob="${MAC_APP_TAG_GLOB:-v[0-9]*}"

if [ "${FORCE:-false}" = "true" ]; then
  echo "changed=true"
  exit 0
fi

# Newest build marker reachable from HEAD. HEAD itself counts: a re-run of an
# already-built commit has the marker at HEAD, so the diff is empty and the
# duplicate is skipped.
anchor="$(git describe --tags --abbrev=0 --match "$marker_glob" HEAD 2>/dev/null || true)"

# Bootstrap fallback: the previous app-release tag. `--match 'v[0-9]*'` excludes
# the SDK module tag series and any other tag namespace.
if [ -z "$anchor" ]; then
  anchor="$(git describe --tags --abbrev=0 --match "$app_tag_glob" HEAD^ 2>/dev/null || true)"
fi

# No anchor at all (first release, or shallow clone) -> build, never silently
# skip a genuine release.
if [ -n "$anchor" ] && git diff --quiet "$anchor" HEAD -- "$apps_dir/"; then
  echo "changed=false"
else
  echo "changed=true"
fi
