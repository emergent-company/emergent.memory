#!/usr/bin/env bash
# Guard migration numbering for changes to apps/server/migrations/*.sql.
#
# WHY THIS IS DISASTER AVOIDANCE, NOT HYGIENE: Goose de-duplicates migrations
# BY VERSION NUMBER and silently skips the later one. If a branch adds a
# migration whose version already exists on origin/main (or in another
# concurrent branch), the second migration NEVER RUNS and the schema diverges
# with no error. Two collisions have already happened: 00180 (#977 vs #981)
# and 00183 (#1057 vs #1053).
#
# Rules, for *.sql files under apps/server/migrations/:
#   1. Existing migrations (present on origin/main) must never be modified,
#      renamed, or deleted. (This rule previously lived only in CI
#      `migration-guard`, so lanes discovered it after pushing.)
#   2. New migrations must be numbered strictly above max(origin/main) AND
#      contiguous ascending starting at max+1 — no gaps, no duplicates.
#
# Offline is tolerated the same way as base-check.sh: warn-and-skip.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

base="origin/main"
mig_dir="apps/server/migrations"

# Resolve the base ref, fetching if needed (offline-tolerant).
if ! git rev-parse --verify --quiet "$base" >/dev/null 2>&1; then
  if ! git fetch --quiet origin main; then
    echo "migration-number: WARNING could not reach origin and no cached $base; skipping" >&2
    exit 0
  fi
fi
if ! git rev-parse --verify --quiet "$base" >/dev/null 2>&1; then
  echo "migration-number: WARNING $base unavailable; skipping" >&2
  exit 0
fi

# Extract the numeric version from a migration filename (basename only).
version_of() {
  printf '%s' "$(basename "$1")" | sed -n 's/^0*\([0-9][0-9]*\)_.*\.sql$/\1/p'
}

# Migration *.sql files present on the base ref, and present now (tracked +
# untracked, excluding ignored). Newline-delimited; paths contain no spaces.
base_files="$(git ls-tree -r --name-only "$base" -- "$mig_dir" | grep '\.sql$' | sort)"
current_files="$(git ls-files --cached --others --exclude-standard -- "$mig_dir" | grep '\.sql$' | sort)"

# Highest version among migrations already on the base ref.
max_base=0
while IFS= read -r f; do
  v="$(version_of "$f")"
  [ -z "$v" ] && continue
  if [ "$v" -gt "$max_base" ]; then max_base="$v"; fi
done <<< "$base_files"

echo "migration-number: max(origin/main) = $max_base"

status=0
added_versions=""

# 1. Existing migrations must not be modified, renamed, or deleted.
while IFS= read -r f; do
  [ -z "$f" ] && continue
  if ! grep -qxF "$f" <<< "$current_files"; then
    echo "migration-number: FAIL existing migration deleted/renamed: $f (Goose has already applied it; create a NEW migration instead)" >&2
    status=1
    continue
  fi
  if ! git diff --quiet "$base" -- "$f"; then
    echo "migration-number: FAIL existing migration modified: $f (Goose has already applied it; create a NEW migration instead)" >&2
    status=1
  fi
done <<< "$base_files"

# 2. New migrations must be numbered above max and contiguous ascending.
while IFS= read -r f; do
  [ -z "$f" ] && continue
  grep -qxF "$f" <<< "$base_files" && continue
  v="$(version_of "$f")"
  if [ -z "$v" ]; then
    echo "migration-number: FAIL new migration does not match NNNNN_name.sql: $f" >&2
    status=1
    continue
  fi
  added_versions="$added_versions $v"
done <<< "$current_files"

# Duplicate versions among the new migrations (clearest signal first).
sorted="$(printf '%s' "$added_versions" | tr ' ' '\n' | sed '/^$/d' | sort -n)"
dupes="$(printf '%s\n' "$sorted" | uniq -d)"
if [ -n "$dupes" ]; then
  echo "migration-number: FAIL duplicate new migration version(s): $(printf '%s' "$dupes" | tr '\n' ' ') (Goose silently skips the later one)" >&2
  status=1
fi

# Above max + contiguous ascending: expect max+1, max+2, ...
expected="$((max_base + 1))"
for v in $(printf '%s\n' "$sorted" | sort -n -u); do
  if [ "$v" -lt "$expected" ]; then
    echo "migration-number: FAIL new migration version $v is not above max($max_base) (a lower/equal version is silently skipped by Goose)" >&2
    status=1
  elif [ "$v" -gt "$expected" ]; then
    echo "migration-number: FAIL gap in new migrations: expected $expected before $v (new migrations must be contiguous ascending)" >&2
    status=1
  fi
  expected="$((v + 1))"
done

if [ "$status" -ne 0 ]; then
  echo "migration-number: FAILED (next free version is $((max_base + 1)))" >&2
  exit 1
fi

echo "migration-number: OK (no migration numbering violations)"
