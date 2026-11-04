#!/usr/bin/env bash
set -euo pipefail

# Generate a channel manifest (manifest.json) from a release dist dir.
# Reads checksums.txt (sha256sum format) and maps each archive to an asset.
#
# Usage: gen-manifest.sh <app> <channel> <dist_dir> <repo> <tag> [version]
#   app       e.g. memory-connector | memory-cli
#   channel   e.g. stable | dev
#   dist_dir  dir containing checksums.txt + archives
#   repo      github "owner/repo"
#   tag       release tag (URL path component)
#   version   optional; if omitted, derived from the first archive filename
#             (works when archives embed the version, e.g. app_1.2.3_os_arch.tar.gz)

APP="${1:?app required}"
CHANNEL="${2:?channel required}"
DIST_DIR="${3:?dist_dir required}"
REPO="${4:?repo required}"
TAG="${5:?tag required}"
VERSION="${6:-}"

command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 1; }

cd "$DIST_DIR"

# GitHub release URLs only need "/" escaped in the tag.
TAG_ENC="${TAG//\//%2F}"

assets="[]"
first=""
while read -r sha fname; do
  [ -z "${fname:-}" ] && continue
  # Match both `app_1.2.3_os_arch.tar.gz` and `app-os-arch.tar.gz` conventions.
  case "$fname" in
    *linux_amd64.tar.gz|*linux-amd64.tar.gz)   os=linux;  arch=amd64 ;;
    *linux_arm64.tar.gz|*linux-arm64.tar.gz)   os=linux;  arch=arm64 ;;
    *darwin_amd64.tar.gz|*darwin-amd64.tar.gz) os=darwin; arch=amd64 ;;
    *darwin_arm64.tar.gz|*darwin-arm64.tar.gz) os=darwin; arch=arm64 ;;
    *) continue ;;
  esac
  [ -z "$first" ] && first="$fname"
  url="https://github.com/${REPO}/releases/download/${TAG_ENC}/${fname}"
  assets="$(printf '%s' "$assets" | jq -c --arg os "$os" --arg arch "$arch" \
    --arg url "$url" --arg sha "$sha" '. + [{os:$os, arch:$arch, url:$url, sha256:$sha}]')"
done < checksums.txt

# Derive version: explicit arg, else strip APP + os/arch from the first archive.
if [ -z "$VERSION" ]; then
  VERSION="$(printf '%s' "$first" \
    | sed -e "s/^${APP}[_-]//" -e 's/\.tar\.gz$//' \
          -e 's/[_-]linux[_-]amd64$//' -e 's/[_-]linux[_-]arm64$//' \
          -e 's/[_-]darwin[_-]amd64$//' -e 's/[_-]darwin[_-]arm64$//')"
fi

jq -n \
  --arg app "$APP" \
  --arg channel "$CHANNEL" \
  --arg version "$VERSION" \
  --arg released_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --argjson assets "$assets" \
  '{app:$app, channel:$channel, version:$version, released_at:$released_at, assets:$assets}' \
  > manifest.json

printf 'wrote manifest.json (app=%s channel=%s version=%s assets=%s)\n' \
  "$APP" "$CHANNEL" "$VERSION" "$(printf '%s' "$assets" | jq 'length')"
