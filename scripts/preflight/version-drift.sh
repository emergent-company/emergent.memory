#!/usr/bin/env bash
# Guard against version drift between the canonical root VERSION file and its
# checked-in references:
#   * the OpenAPI `// @version` annotation in apps/server/cmd/server/main.go
#   * every MARKETING_VERSION in apps/ios/VoiceAgent.xcodeproj/project.pbxproj
#     (VoiceAgent app, Debug + Release configs)
#   * the committed swagger spec's `info.version` in apps/server/docs/swagger
#     (derived from the `@version` annotation by `task server:swagger`)
#
# Root VERSION is the single canonical source of the release version; a release
# must bump all references together (see the `release` task in Taskfile.yml).
# Fails with a clear message when any of them diverge.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

pbx="apps/ios/VoiceAgent.xcodeproj/project.pbxproj"
swagger_file="apps/server/docs/swagger/swagger.json"

root_version="$(tr -d '[:space:]' < VERSION)"

api_line="$(grep -m1 '@version' apps/server/cmd/server/main.go || true)"
api_version="$(printf '%s' "$api_line" | sed -E 's#.*@version[[:space:]]*##' | tr -d '[:space:]')"

swagger_version="$(jq -r '.info.version // empty' "$swagger_file" 2>/dev/null || true)"

if [ -z "$root_version" ] || [ -z "$api_version" ] || [ -z "$swagger_version" ]; then
  echo "version drift guard: could not read VERSION, @version annotation, and/or swagger spec version" >&2
  exit 1
fi

status=0

if [ "$root_version" != "$api_version" ]; then
  echo "version drift: root VERSION=$root_version != apps/server/cmd/server/main.go @version=$api_version" >&2
  status=1
fi

if [ "$root_version" != "$swagger_version" ]; then
  echo "version drift: root VERSION=$root_version != $swagger_file info.version=$swagger_version" >&2
  status=1
fi

ios_versions="$(sed -nE 's/^[[:space:]]*MARKETING_VERSION = ([^;]+);.*/\1/p' "$pbx")"
if [ -z "$ios_versions" ]; then
  echo "version drift guard: no MARKETING_VERSION found in $pbx" >&2
  status=1
else
  for mv in $ios_versions; do
    if [ "$mv" != "$root_version" ]; then
      echo "version drift: $pbx MARKETING_VERSION=$mv != root VERSION=$root_version" >&2
      status=1
    fi
  done
fi

if [ "$status" -ne 0 ]; then
  echo "Bump VERSION, @version, all iOS MARKETING_VERSION values, and regenerate the swagger spec together — see the 'release' task in Taskfile.yml." >&2
  exit 1
fi

echo "version aligned: $root_version"
