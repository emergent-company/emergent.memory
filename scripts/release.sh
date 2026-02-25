#!/usr/bin/env bash
# release.sh — bump version, tag, and push to trigger CI
#
# Usage:   bash scripts/release.sh <new-version>
# Example: bash scripts/release.sh 0.28.0
#
# What this does:
#   1. Updates VERSION and apps/server-go/cmd/server/main.go
#   2. Commits the bump
#   3. Tags v<new> (triggers emergent-cli.yml + publish-minimal-images.yml)
#   4. Tags apps/server-go/pkg/sdk/v<new> (Go module proxy)
#   5. Pushes the commit and both tags

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

# ── Validate input ────────────────────────────────────────────────────────────

NEW="${1:-}"
if [[ -z "$NEW" ]]; then
  echo "Usage: $0 <new-version>  (e.g. 0.28.0)"
  echo "Current version: $(cat VERSION)"
  exit 1
fi

if ! [[ "$NEW" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: version must be X.Y.Z without a leading 'v' (got: $NEW)"
  exit 1
fi

CURRENT="$(cat VERSION)"

# Require new > current (lexicographic semver comparison via sort -V)
LOWER="$(printf '%s\n%s' "$CURRENT" "$NEW" | sort -V | head -1)"
if [[ "$LOWER" != "$CURRENT" ]] || [[ "$CURRENT" == "$NEW" ]]; then
  echo "Error: new version ($NEW) must be greater than current ($CURRENT)"
  exit 1
fi

# ── Safety checks ─────────────────────────────────────────────────────────────

BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$BRANCH" != "main" && "$BRANCH" != "master" ]]; then
  echo "Error: must be on main/master branch (currently on '$BRANCH')"
  exit 1
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "Error: working tree has uncommitted changes. Commit or stash them first."
  git status --short
  exit 1
fi

MAIN_GO="apps/server-go/cmd/server/main.go"
if ! grep -q "// @version $CURRENT" "$MAIN_GO"; then
  echo "Error: expected '// @version $CURRENT' in $MAIN_GO — not found."
  echo "Update the file manually before re-running this script."
  exit 1
fi

# ── Execute release ───────────────────────────────────────────────────────────

echo "Releasing $CURRENT → $NEW"
echo ""

echo "▸ Updating VERSION..."
echo "$NEW" > VERSION

echo "▸ Updating $MAIN_GO swagger annotation..."
sed -i "s|// @version $CURRENT|// @version $NEW|g" "$MAIN_GO"

echo "▸ Committing..."
git add VERSION "$MAIN_GO"
git commit -m "chore: bump version to $NEW"

echo "▸ Tagging..."
git tag "v$NEW"
git tag "apps/server-go/pkg/sdk/v$NEW"

echo "▸ Pushing commit..."
git push origin "$BRANCH"

echo "▸ Pushing tags..."
git push origin "v$NEW" "apps/server-go/pkg/sdk/v$NEW"

# ── Summary ───────────────────────────────────────────────────────────────────

echo ""
echo "Released v$NEW"
echo ""
echo "Tags pushed:"
echo "  v$NEW                         → emergent-cli.yml (CLI binaries + GitHub Release)"
echo "  apps/server-go/pkg/sdk/v$NEW  → Go module proxy"
echo ""
echo "CI will build:"
echo "  - Cross-platform CLI binaries (linux, darwin, windows, freebsd × amd64/arm64)"
echo "  - Docker images (ghcr.io/emergent-company/emergent-server-with-cli:$NEW)"
echo "  - Docker images (ghcr.io/emergent-company/emergent-cli:$NEW)"
