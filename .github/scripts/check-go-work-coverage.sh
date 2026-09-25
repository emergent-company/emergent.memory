#!/usr/bin/env bash
# Assert every go.mod in the repo (outside vendor/) is listed in go.work's
# `use` block, so workspace-wide commands (`go build ./...`, linters driven by
# the workspace) cannot silently skip a module. Also rejects stale `use`
# entries that point at a directory with no go.mod.
#
# See issue: go.work drift lets a broken module go unnoticed (same class as
# #923). This guard is wired into Meta CI (ci.yml), which runs on every PR.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

entries="$(mktemp)"
trap 'rm -f "$entries"' EXIT

# All `use` entries in go.work, one relative path per line.
sed -n '/^use (/,/^)/p' go.work |
  sed -n 's/^[[:space:]]*\(\.\/[^[:space:]]*\)[[:space:]]*$/\1/p' >"$entries"

status=0

# 1. Every go.mod (outside vendor/) must appear in go.work.
while IFS= read -r modfile; do
  dir="${modfile%/go.mod}"
  if ! grep -qxF "$dir" "$entries"; then
    printf 'go.work missing module: %s\n' "$dir" >&2
    status=1
  fi
done < <(find . -name go.mod -not -path './vendor/*' -not -path '*/vendor/*' | sort)

# 2. No go.work entry may point at a directory lacking a go.mod.
while IFS= read -r dir; do
  if [ ! -f "$dir/go.mod" ]; then
    printf 'go.work entry has no go.mod: %s\n' "$dir" >&2
    status=1
  fi
done <"$entries"

if [ "$status" -ne 0 ]; then
  echo "go.work coverage check FAILED" >&2
  exit 1
fi

echo "go.work coverage check OK: all modules accounted for"
