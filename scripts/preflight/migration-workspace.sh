#!/usr/bin/env bash
# Fail fast when apps/server/migrations is not safe to embed.
#
# goose v3.26 with `//go:embed *.sql` PANICS at load when two migrations share a
# version (`panic: goose: duplicate version N detected`), and a stray untracked
# migration left in a reused self-hosted runner workspace gets embedded too. This
# runs in CI immediately before the migrate / unit-test steps so the failure is a
# clear, actionable message instead of an opaque goose panic.
#
# Base-independent (unlike migration-number.sh): it inspects the working tree
# only, so it needs no network and no origin/main. Deliberately NOT part of local
# scripts/preflight/all.sh -- an uncommitted new migration is normal in dev.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"
mig_dir="apps/server/migrations"

# 1. No stray untracked *.sql (reused-runner workspace contamination).
untracked="$(git ls-files --others --exclude-standard -- "$mig_dir")"
if [ -n "$untracked" ]; then
  echo "migration-workspace: FAIL untracked file(s) under $mig_dir:" >&2
  printf '%s\n' "$untracked" >&2
  echo "migration-workspace: FAIL these would be embedded by //go:embed and can make goose panic at load." >&2
  exit 1
fi

# 2. Unique, well-formed versions across the tracked migration set.
files="$(git ls-files --cached -- "$mig_dir" | grep -E '\.sql$' | sort || true)"
if [ -z "$files" ]; then
  echo "migration-workspace: FAIL no tracked migrations found under $mig_dir" >&2
  exit 1
fi

versions=""
while IFS= read -r f; do
  [ -z "$f" ] && continue
  b="$(basename "$f")"
  v="$(printf '%s' "$b" | sed -nE 's/^([0-9]{5})_[a-z0-9_]+\.sql$/\1/p')"
  if [ -z "$v" ]; then
    echo "migration-workspace: FAIL $b does not match NNNNN_lowercase_name.sql" >&2
    exit 1
  fi
  versions="$versions $v"
done <<< "$files"

dupes="$(printf '%s' "$versions" | tr ' ' '\n' | sed '/^$/d' | sort | uniq -d)"
if [ -n "$dupes" ]; then
  echo "migration-workspace: FAIL duplicate migration version(s): $(printf '%s' "$dupes" | tr '\n' ' ')" >&2
  printf '%s\n' "$files" | grep -E "/0*($(printf '%s' "$dupes" | paste -sd'|' -))_[a-z0-9_]+\.sql$" >&2 || true
  echo "migration-workspace: FAIL goose panics at load when two migrations share a version." >&2
  exit 1
fi

count="$(printf '%s\n' "$files" | sed '/^$/d' | wc -l | tr -d ' ')"
echo "migration-workspace: OK ($count migrations, versions unique, no stray files)"
