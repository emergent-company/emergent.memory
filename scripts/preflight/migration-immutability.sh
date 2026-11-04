#!/usr/bin/env bash
# Guard that no EXISTING migration (present on the base branch) is modified,
# renamed, or deleted — with one narrow, documented exception.
#
# WHY THIS MATTERS: Goose records applied migrations by numeric version in
# goose_db_version. Renaming or editing a migration that has already been applied
# makes Goose skip it on any environment that recorded the old version (silent
# schema divergence); deleting it makes the embedded migration set inconsistent.
# This is the single implementation of the immutability rule: the local preflight
# (scripts/preflight/migration-number.sh) calls this script, and CI's
# `Migration Immutability` job (.github/workflows/server.yml) calls this same
# script, so both gates always agree.
#
# NARROW EXCEPTION — duplicate-version repair (REPAIR):
#   Two migrations once merged sharing version 227 (issue #1646). A duplicate
#   version makes goose PANIC at startup (`goose: duplicate version 227
#   detected`) before applying anything, so no environment can ever have applied
#   the later file — renumbering it is therefore safe. The exception applies to
#   removing/renaming a base-branch file F with version V only when BOTH hold:
#
#     * V is duplicated among the base-branch migrations (>=2 base files share V), and
#     * the PR adds a contiguous replacement migration whose versions run
#       max(base)+1 .. max(base)+N with no gaps and no duplicates (the renumbered
#       successor is contiguous with the base sequence).
#
#   When both hold, a REPAIR notice is printed and the rule does not fail for
#   that file. Everything else is unchanged: a plain rename/modify/delete of a
#   NON-duplicated migration still fails, and modifying a migration is NEVER
#   exempt.
#
# Usage: scripts/preflight/migration-immutability.sh [--base <ref>]
#
# Offline is tolerated the same way as the sibling preflight scripts
# (migration-number.sh, base-check.sh): warn-and-skip when the base ref cannot
# be resolved.
set -euo pipefail

base="origin/main"
while [ $# -gt 0 ]; do
  case "$1" in
    --base) base="${2:?--base requires a ref}"; shift 2 ;;
    --base=*) base="${1#--base=}"; shift ;;
    -h|--help) echo "usage: $0 [--base <ref>]"; exit 0 ;;
    *) echo "migration-immutability: unknown argument: $1" >&2; exit 2 ;;
  esac
done

root="$(git rev-parse --show-toplevel)"
cd "$root"

mig_dir="apps/server/migrations"

# Refresh the base ref. A stale local ref is exactly the hazard this guard
# exists to catch. Tolerate no-network gracefully: fall back to a cached ref, or
# skip when none exists.
branch="${base#origin/}"
if ! git fetch --quiet --no-tags origin "$branch" 2>/dev/null; then
  if git rev-parse --verify --quiet "$base" >/dev/null 2>&1; then
    echo "migration-immutability: WARNING could not reach origin; using cached $base" >&2
  else
    echo "migration-immutability: WARNING could not reach origin and no cached $base; skipping" >&2
    exit 0
  fi
fi
if ! git rev-parse --verify --quiet "$base" >/dev/null 2>&1; then
  echo "migration-immutability: WARNING $base unavailable; skipping" >&2
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

# Added migration versions (present now, absent on base), sorted ascending and
# de-duplicated. These are the PR's new/replacement migrations.
added_versions=""
while IFS= read -r f; do
  [ -z "$f" ] && continue
  if ! grep -qxF "$f" <<< "$base_files"; then
    v="$(version_of "$f")"
    [ -n "$v" ] && added_versions="$added_versions $v"
  fi
done <<< "$current_files"
added_versions="$(printf '%s' "$added_versions" | tr ' ' '\n' | sed '/^$/d' | sort -n -u)"

# True (exit 0) when the PR's added versions form a contiguous run beginning at
# max(base)+1: exactly max+1, max+2, ... with no gaps and no duplicates. This is
# the same contiguity rule 2 enforces in migration-number.sh; applying it here
# too keeps the exception from ever permitting a gap.
added_contiguous() {
  [ -n "$added_versions" ] || return 1
  local expected=$((max_base + 1)) v
  while IFS= read -r v; do
    [ "$v" = "$expected" ] || return 1
    expected=$((expected + 1))
  done <<< "$added_versions"
  return 0
}

status=0

while IFS= read -r f; do
  [ -z "$f" ] && continue

  if ! grep -qxF "$f" <<< "$current_files"; then
    # Removed or renamed on this branch.
    v="$(version_of "$f")"
    dup_count=0
    while IFS= read -r bf; do
      [ -z "$bf" ] && continue
      [ "$(version_of "$bf")" = "$v" ] && dup_count=$((dup_count + 1))
    done <<< "$base_files"

    if [ -n "$v" ] && [ "$dup_count" -ge 2 ] && added_contiguous; then
      echo "migration-immutability: REPAIR duplicate base migration version $v: removing/renaming $f is permitted because $dup_count base migrations share version $v and the PR adds the contiguous replacement version(s) ${added_versions// /,} above max($base) ($max_base)" >&2
      continue
    fi

    echo "migration-immutability: FAIL existing migration deleted/renamed: $f (Goose records versions by number; renumbering an applied migration silently skips it. Only a duplicated base version repaired by a contiguous successor is exempt — see the header of scripts/preflight/migration-immutability.sh)" >&2
    status=1
    continue
  fi

  if ! git diff --quiet "$base" -- "$f"; then
    echo "migration-immutability: FAIL existing migration modified: $f (Goose has already applied it; create a NEW migration instead)" >&2
    status=1
  fi
done <<< "$base_files"

if [ "$status" -ne 0 ]; then
  echo "migration-immutability: FAILED" >&2
  exit 1
fi

echo "migration-immutability: OK (no existing migration modified, renamed, or deleted)"
