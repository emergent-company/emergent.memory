#!/usr/bin/env bash
# A contributor note that names the removed NestJS tree (apps/server/src/...)
# must declare itself historical. The paths it quotes no longer exist, so an
# unbannered note reads as current guidance to anyone who finds it by search
# (#2159): 45 files were in that state, describing webhooks, OAuth flows and
# prompt pipelines that the Go server does not implement the same way.
#
# The marker is deliberately boring and greppable:
#
#   <!-- status: historical (pre-Go) -->
#
# docs/archive/ is exempt — everything under it is historical by definition.
# A note that is rewritten for the Go server simply drops the marker and stops
# referencing apps/server/src, which is the state this gate is driving toward.
#
# Run from anywhere; resolves the repo root itself.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

marker='status: historical (pre-Go)'
failed=0

while IFS= read -r file; do
	case "$file" in
	docs/archive/*) continue ;;
	# The map states the rule itself, so it necessarily names the tree.
	docs/README.md) continue ;;
	esac
	# Two conditions, so the marker alone cannot satisfy the gate: the reader
	# must actually see a historical statement near the top of the page.
	if ! grep -qF "$marker" "$file"; then
		echo "docs-stale-paths: $file names apps/server/src without a historical banner marker"
		failed=1
	# Strip the marker comment: it contains the word "historical", and would
	# otherwise satisfy its own check.
	elif ! head -n 15 "$file" | sed 's/<!--.*-->//' | grep -qi 'historical'; then
		echo "docs-stale-paths: $file carries the marker but no visible historical statement"
		failed=1
	fi
done < <(grep -rl --include='*.md' 'apps/server/src' docs || true)

if ((failed)); then
	cat >&2 <<'EOF'

Fix one of:
  - add the banner at the top of the note:
        > **Status: historical design note (pre-Go).** ...
        <!-- status: historical (pre-Go) -->
  - keep the note out of docs/ — internal design history belongs in the
    infrastructure repository, not next to published documentation
  - rewrite it against the Go server and drop the apps/server/src references
EOF
	exit 1
fi
