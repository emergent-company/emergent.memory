#!/usr/bin/env bash
# Guard migration numbering for changes to apps/server/migrations/*.sql.
#
# WHY THIS IS DISASTER AVOIDANCE, NOT HYGIENE: with goose v3.26.0 and
# `//go:embed *.sql`, a duplicate version makes goose PANIC at startup —
# `panic: goose: duplicate version N detected` — so the process never comes up.
# (Older goose releases de-duplicated by version number and silently skipped the
# later one; this repro's goose fails loudly instead. Either way, a collision
# means the second migration never applies and the schema diverges.) If a branch
# adds a migration whose version already exists on the base branch (or in another
# concurrent branch), startup panics on any environment that embeds both files.
# Two collisions have already happened: 00180 (#977 vs #981) and 00183
# (#1057 vs #1053).
#
# Rules, for *.sql files under apps/server/migrations/:
#   1. Existing migrations (present on the base branch) must never be modified,
#      renamed, or deleted. This rule is implemented once, in
#      scripts/preflight/migration-immutability.sh, which this script calls and
#      which CI's `Migration Immutability` job also calls — so the preflight and
#      CI gates always agree. (It includes a narrow, documented exception for
#      repairing an already-merged duplicate base version; see that script's
#      header.)
#   2. New migrations must be numbered strictly above max(base) AND
#      contiguous ascending starting at max+1 — no gaps, no duplicates.
#   3. A new migration must not build a plain `CREATE INDEX` on a table that
#      already exists on the base branch: on a populated table that takes a
#      SHARE lock and blocks writes for the whole build. Use
#      `CREATE INDEX CONCURRENTLY` (`-- +goose NO TRANSACTION`) and the
#      drop-then-create + post-condition guard from 00012/00161/00170 — see
#      apps/server/migrations/README.md, "Build indexes CONCURRENTLY". Only
#      *added* migrations are checked — the plain index builds already applied
#      by earlier migrations are applied debt and are deliberately not rewritten
#      (issue #844, section 2). Both the Up and the Down are inspected: a
#      rollback that rebuilds an index on a populated table locks it just the
#      same.
#      For a deliberate exception, put this on the line before the statement:
#        -- index-concurrently-allowed: <reason>
#
# Offline is tolerated the same way as base-check.sh: warn-and-skip.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

base_branch="${PREFLIGHT_BASE:-${GITHUB_BASE_REF:-dev}}"
base="origin/$base_branch"
mig_dir="apps/server/migrations"

# Refresh the remote tracking ref first — a stale local base ref is exactly
# the hazard this guard exists to catch (a worktree handed an outdated base would
# otherwise compare against an old ref and MISS a real collision). Tolerate
# no-network gracefully: fall back to a cached ref, or skip when none exists.
if ! git fetch --quiet origin "$base_branch"; then
  if git rev-parse --verify --quiet "$base" >/dev/null 2>&1; then
    echo "migration-number: WARNING could not reach origin; using cached $base" >&2
  else
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

echo "migration-number: max($base) = $max_base"

status=0
numbering_bad=0
added_versions=""

# 1. Existing migrations must not be modified, renamed, or deleted. Single
#    implementation (shared with the CI immutability job):
#    scripts/preflight/migration-immutability.sh. It prints a REPAIR notice and
#    succeeds for the narrow duplicated-version repair case.
if ! bash "$root/scripts/preflight/migration-immutability.sh" --base "$base"; then
  status=1
fi

# 2. New migrations must be numbered above max and contiguous ascending.
while IFS= read -r f; do
  [ -z "$f" ] && continue
  grep -qxF "$f" <<< "$base_files" && continue
  v="$(version_of "$f")"
  if [ -z "$v" ]; then
    echo "migration-number: FAIL new migration does not match NNNNN_name.sql: $f" >&2
    status=1
    numbering_bad=1
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
  numbering_bad=1
fi

# Above max + contiguous ascending: expect max+1, max+2, ...
expected="$((max_base + 1))"
for v in $(printf '%s\n' "$sorted" | sort -n -u); do
  if [ "$v" -lt "$expected" ]; then
    echo "migration-number: FAIL new migration version $v is not above max($max_base) (a lower/equal version is silently skipped by Goose)" >&2
    status=1
    numbering_bad=1
  elif [ "$v" -gt "$expected" ]; then
    echo "migration-number: FAIL gap in new migrations: expected $expected before $v (new migrations must be contiguous ascending)" >&2
    status=1
    numbering_bad=1
  fi
  expected="$((v + 1))"
done

# 3. A new migration must not build a plain CREATE INDEX on a table that already
#    exists on the base branch (SHARE lock for the whole build on a populated
#    table). Tables created by the same set of new migrations are exempt, because
#    they are empty — that is what the base-table census below approximates. The
#    census is every CREATE TABLE name in the base migrations, so it also covers
#    tables that were later dropped; a migration that re-creates one of those and
#    indexes it plainly needs the escape hatch.
census_rc=0
base_tables_raw="$(git grep -hoiE 'CREATE TABLE( IF NOT EXISTS)?[[:space:]]+([a-z0-9_]+\.)?[a-z0-9_]+' "$base" -- "$mig_dir" 2>/dev/null)" || census_rc=$?
check_indexes=1
if [ "$census_rc" -gt 1 ]; then
  echo "migration-number: WARNING base table census failed (git grep exit $census_rc); skipping the CONCURRENTLY check" >&2
  check_indexes=0
elif [ -z "$base_tables_raw" ]; then
  echo "migration-number: WARNING base table census is empty; skipping the CONCURRENTLY check" >&2
  check_indexes=0
fi
base_tables="$(printf '%s\n' "$base_tables_raw" \
  | sed -E 's/.*[[:space:]]([a-z0-9_]+\.)?([a-z0-9_]+)$/\2/' | tr '[:upper:]' '[:lower:]' | sort -u)"

# Emit "<file>:<line>: <table>" for each offending statement in one migration
# file. Statements are split on `;` so a wrapped `CREATE INDEX ... \n ON kb.t
# (...)` is caught, and also so two statements sharing a line cannot mask each
# other (a `DROP INDEX CONCURRENTLY` next to a plain create used to hide it).
# `--` and `/* ... */` comments are stripped, double quotes removed (so
# "kb"."invites" resolves), and matching is done on a lowercased copy: mawk 1.3.4
# mis-matches long runs of single-character bracket classes
# (e.g. /[Cc][Oo][Nn][Cc].../), which silently defeated the CONCURRENTLY
# exclusion.
index_offenders() {
  awk -v tables="$base_tables" '
    BEGIN { n = split(tables, t, "\n"); for (i = 1; i <= n; i++) if (t[i] != "") base[t[i]] = 1 }
    function check(stmt, at) {
      if (stmt !~ /create[ \t]+(unique[ \t]+)?index[ \t]/) return
      if (stmt ~ /concurrently/) return
      if (!match(stmt, /on[ \t]+(only[ \t]+)?([a-z0-9_]+\.)?[a-z0-9_]+/)) return
      tbl = substr(stmt, RSTART, RLENGTH)
      sub(/^on[ \t]+(only[ \t]+)?/, "", tbl)
      sub(/^.*\./, "", tbl)
      if (tbl in base) printf "%s:%d: CREATE INDEX on pre-existing table %s is not CONCURRENTLY\n", FILENAME, at, tbl
    }
    {
      line = $0
      if (inb) {
        p = index(line, "*/")
        if (p == 0) next
        line = substr(line, p + 2)
        inb = 0
      }
      while ((p = index(line, "/*")) > 0) {
        rest = substr(line, p + 2)
        q = index(rest, "*/")
        if (q == 0) { line = substr(line, 1, p - 1); inb = 1; break }
        line = substr(line, 1, p - 1) substr(rest, q + 2)
      }
      if (tolower(line) ~ /^[ \t]*--[ \t]*index-concurrently-allowed:/) { pending = 1; next }
      sub(/--.*/, "", line)
      line = tolower(line)
      gsub(/"/, "", line)
      if (line !~ /[^ \t]/) next
      if (buf == "") at = NR
      buf = buf " " line
      while ((p = index(buf, ";")) > 0) {
        stmt = substr(buf, 1, p - 1)
        buf = substr(buf, p + 1)
        if (stmt ~ /[a-z0-9_]/) { if (!pending) check(stmt, at); pending = 0 }
      }
    }
  ' "$1" || true
}

if [ "$check_indexes" -eq 1 ]; then
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    grep -qxF "$f" <<< "$base_files" && continue
    [ -f "$f" ] || continue
    offenders="$(index_offenders "$f")"
    if [ -n "$offenders" ]; then
      while IFS= read -r o; do
        echo "migration-number: FAIL $o" >&2
      done <<< "$offenders"
      echo "migration-number: FAIL use CREATE INDEX CONCURRENTLY (-- +goose NO TRANSACTION) for a pre-existing table; 00012/00161/00170 are the pattern." >&2
      echo "migration-number: FAIL if the exception is deliberate, put '-- index-concurrently-allowed: <reason>' on the line before the statement." >&2
      status=1
    fi
  done <<< "$current_files"
fi

if [ "$status" -ne 0 ]; then
  if [ "$numbering_bad" -ne 0 ]; then
    echo "migration-number: FAILED (next free version is $((max_base + 1)))" >&2
  else
    echo "migration-number: FAILED (see the violation(s) above; numbering itself is valid)" >&2
  fi
  exit 1
fi

echo "migration-number: OK (no migration numbering violations)"
