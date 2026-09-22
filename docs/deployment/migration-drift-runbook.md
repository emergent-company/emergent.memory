# Runbook — Migration drift on dev (`00164` recorded-but-no-op) and local backlog catch-up

Covers issue **#734** and the local `memtest-db` migration backlog.

- **Environment:** remote dev = host `emergent-dev`, containers `emergent-dev-memory-server-1`,
  `emergent-dev-memory-db-1` (pg16.11 / pgvector 0.8.1, db `emergent`).
- **Server image:** `ghcr.io/emergent-company/memory-server:dev` (mutable tag).
- **Migrations run at server container startup** (`goose.UpContext`, embedded FS). There is
  **no separate migrator service** in `/opt/emergent-dev/docker-compose.yml`; the
  `memory-dev-mcp.migrate_db` tool would try `docker compose run --rm migrator` and has no
  such service on dev.
- **Local shared DB:** `memtest-db` (`localhost:5432`, db `emergent`). Shared with other
  sessions — do not mutate without approval.

## 1. Symptom

`public.goose_db_version` records `164` as applied, but the catalog has **no HNSW index
anywhere** and the legacy `kb."IDX_graph_objects_embedding_v2_ivfflat"` is still present.

```
 version_id | is_applied |           tstamp
------------+------------+----------------------------
        163 | t          | 2026-09-22 07:35:12.945823
        164 | t          | 2026-09-22 10:35:39.845488
        165 | t          | 2026-09-22 14:04:53.956971
        172 | t          | 2026-09-22 14:04:54.152619
```

Key catalog facts (read-only):

| object | am | indisvalid | size | file mtime |
|---|---|---|---|---|
| `kb.IDX_graph_objects_embedding_v2_ivfflat` | ivfflat | t | 420 MB | **2026-09-22 02:04:42 UTC** |
| any `hnsw` index | — | — | — | **none anywhere** |

`SELECT count(*) FROM pg_index WHERE NOT indisvalid OR NOT indisready;` → `0` (no leftover
INVALID indexes). `kb.graph_objects` = 107,938 rows, 862 MB heap / 1.5 GB total.

## 2. Root cause (evidence-supported)

**The `164` version row was written out-of-band — no `00164` Up ever executed to completion
on dev.**

Chain of evidence:

1. **The image's migration file is not the problem.** Byte-for-byte identical to the repo:
   ```
   image  /app/migrations/00164_hnsw_graph_objects_embedding.sql
     sha256 dc965c8c90bc8d821993d2a4bde771491cbefe810f42d817b4db4cfbf357c37f
   repo   apps/server/migrations/00164_hnsw_graph_objects_embedding.sql
     sha256 dc965c8c90bc8d821993d2a4bde771491cbefe810f42d817b4db4cfbf357c37f
   ```

2. **The ivfflat index was never touched by `00164`.** Its backing file
   (`base/16384/369952`) has mtime **2026-09-22 02:04:42**, i.e. *before* the `164` stamp.
   `00164` Up ends with `DROP INDEX CONCURRENTLY kb."IDX_graph_objects_embedding_v2_ivfflat"`,
   so if Up had run, that file would be gone. It is not.

3. **The HNSW build was repeatedly killed.** `docker logs emergent-dev-memory-db-1` for
   2026-09-22 shows the `00164` statement being terminated **three times**:
   ```
   10:14:45.719 UTC [329552] FATAL:  terminating connection due to administrator command
       STATEMENT:  CREATE INDEX CONCURRENTLY IF NOT EXISTS "IDX_graph_objects_embedding_v2_hnsw"
                       ON kb.graph_objects USING hnsw (embedding_v2 vector_cosine_ops)
                       WITH (m = 16, ef_construction = 64);
   10:34:21.699 UTC [330102] FATAL:  terminating connection due to administrator command   (same statement)
   10:35:49.839 UTC [331777] FATAL:  terminating connection due to administrator command   (same statement)
   ```
   (`administrator command` = `pg_terminate_backend`; the DB container itself stayed up 4 days,
   so this is not a postmaster restart.)

4. **goose cannot record a terminated migration.** goose `v3.26.0`
   (`github.com/pressly/goose/v3@v3.26.0/migration_sql.go`, NO TRANSACTION path) runs each
   statement with `db.ExecContext`, returns on the **first** error, and only then calls
   `InsertVersionNoTx`. A terminated `CREATE INDEX CONCURRENTLY` returns an error ⇒ goose
   aborts ⇒ **no version row**. Yet a row exists at `10:35:39`, sandwiched between the
   `10:34:21` and `10:35:49` terminations.

5. **No `00164` variant can produce the observed state.** Every historical body (v1 at
   `a15e4c348`, v2 at `fb73ab930`, final at `ab32fc1dd`/PR #707) either leaves the HNSW index
   present or fails:

   | body | DROP hnsw | CREATE hnsw | DROP ivfflat | result |
   |---|---|---|---|---|
   | v1 (unqualified names) | no-op (search_path ≠ `kb`) | creates `kb` hnsw | no-op | hnsw **present** |
   | v2 (`kb."…"` CREATE) | drops | **errors** (qualified index name) | — | not recorded |
   | final (PR #707) | drops | creates | drops | hnsw present, ivfflat gone |

   Observed state = hnsw absent **and** ivfflat present **and** no invalid index. Not reachable
   by execution of any variant.

**Conclusion:** `goose_db_version` was written **without executing the migration** — via the
repo's own non-executing path `migrate -c mark-applied -v 164` /
`Migrator.MarkApplied` (`apps/server/internal/migrate/migrate.go`), which does
`INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES ($1, true, now())`,
or an equivalent manual INSERT. An operator/tool iterating on the long CONCURRENTLY build
(terminating it at 10:14/10:34/10:35) then marked the version applied to unblock the deploy.

### Ruled out

- **(a) image file differs from `origin/main`** — ruled out: shas match (step 1 above). The
  current `:dev` image is byte-identical. (An *older* `:dev` digest at 10:35 cannot be
  re-inspected — the tag is mutable — but even the pre-fix v1/v2 bodies cannot yield the
  observed state; see the table.)
- **(b) NO TRANSACTION body silently no-op'ing while goose recorded** — ruled out by goose
  source (step 4) and by the fact that the final body's `CREATE INDEX … IF NOT EXISTS` on a
  non-existent index must create it.
- **(c) a later `00164` Down applied without goose rewriting the row** — ruled out: goose's
  Down path calls `DeleteVersionNoTx` (removes the row), and the ivfflat file mtime
  (02:04:42) predates the 10:35:39 stamp, so no Down rebuild occurred afterwards.
- **(d) version row written by a different tool/path** — **supported** (the remaining,
  evidence-consistent explanation).

### Uncertain / not provable

- The exact writer of the row (the `mark-applied` CLI vs a raw INSERT vs an ops automation)
  cannot be uniquely identified: the DB ran `log_statement=none`, and the PG WAL for the
  10:35 window is already recycled (oldest retained segment is from 14:05). WAL forensics was
  therefore not possible.
- Which actor issued `pg_terminate_backend` is not logged. `memory-dev-mcp` cannot (its SQL
  tool is read-only; `migrate_db` only runs a compose migrator service that does not exist on
  dev), so it was a human/agent action outside the toolset.

## 3. Second defect on dev — crash loop from out-of-order merges

The server container is currently restart-looping (12 restarts, exit 1):

```
Running database migrations...
Error running migrations: error: found 2 missing migrations before current version 172:
	version 170: 00170_embedding_indexes_hnsw.sql
	version 171: 00171_graph_relationships_embedding_hnsw.sql
```

Cause: **migrations merged out of version order.** On `origin/main` (first-parent):

| migration | merged into main |
|---|---|
| `00172_graph_relationships_namespace` (#729) | 2026-09-22 **15:48:29** +02:00 |
| `00170_embedding_indexes_hnsw` (#727) | 2026-09-22 16:13:35 +02:00 |
| `00171_graph_relationships_embedding_hnsw` (#731) | 2026-09-22 16:18:13 +02:00 |
| `00174_graph_objects_fts_identifiers` (#728) | 2026-09-22 16:33:58 +02:00 |

A dev deploy at `2026-09-22 14:04:53 UTC` (16:04 CEST) built from a main that already had
`00172` but not yet `00170`/`00171`, so goose applied `165` then `172` (200 ms apart — 172 is
cheap: `ADD COLUMN` + a partial index whose `WHERE namespace IS NOT NULL` matched 0 rows).
When `00170`/`00171` later landed, goose's missing-migration guard refused to start.

Dev's applied set is therefore: `…160,161,162,163,164,165,172` — **missing `170,171,173,174`**.

## 4. Remediation (presented, NOT executed)

> All of the following mutate dev. Requires explicit approval. Everything above this point was
> read-only.

### 4a. `00164` index swap (issue #734)

Run each statement **separately / autocommit** — `CONCURRENTLY` cannot run inside a
transaction block (this is exactly why the migration is `-- +goose NO TRANSACTION`).

```sql
-- optional, recommended: 64 MB maintenance_work_mem is low for an HNSW build
SET maintenance_work_mem = '512MB';

CREATE INDEX CONCURRENTLY IF NOT EXISTS "IDX_graph_objects_embedding_v2_hnsw"
    ON kb.graph_objects USING hnsw (embedding_v2 vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

DROP INDEX CONCURRENTLY IF EXISTS kb."IDX_graph_objects_embedding_v2_ivfflat";
```

Verified verbatim on a scratch pgvector pg16 container (simulated dev state → created
`kb."IDX_graph_objects_embedding_v2_hnsw"` → dropped the ivfflat → only hnsw remained,
`indisvalid=t`).

**Verification (assert `indisvalid`):**

```sql
SELECT c.relname, am.amname, i.indisvalid, i.indisready
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_am am ON am.oid = c.relam
WHERE n.nspname = 'kb'
  AND c.relname IN ('IDX_graph_objects_embedding_v2_hnsw',
                    'IDX_graph_objects_embedding_v2_ivfflat');
-- expect exactly one row: hnsw, indisvalid=t, indisready=t; ivfflat row ABSENT
```

**Lock / build-time risk (108k rows, 768 dim, 420 MB ivfflat):**

- `CREATE/DROP INDEX CONCURRENTLY` take **`SHARE UPDATE EXCLUSIVE`** on the table: they do
  **not** block `SELECT/INSERT/UPDATE/DELETE`, but they **do** block other DDL, `VACUUM FULL`,
  `REINDEX`, and any other concurrent build on the table. No `ACCESS EXCLUSIVE` window.
- Build duration: minutes (parallel, `max_parallel_maintenance_workers=2`); with only
  `maintenance_work_mem=64 MB` it will be slower — set `512 MB`+ first.
- Transient storage: a second ANN index (~330–450 MB) exists until the ivfflat is dropped.
  Dev has **78 GB free** — ample.
- If a build is interrupted it leaves an **INVALID** index; `DROP INDEX [CONCURRENTLY] IF
  EXISTS` before retrying (the migration already does this). `statement_timeout=0`, so the
  build will not be cancelled by a timeout — only an external `pg_terminate_backend` can kill
  it, so stop any such actor first.
- `kb.graph_objects.embedding_v2` must be non-null-populated; an empty/partial vector column
  builds a valid-but-useless index (verify recall separately if needed).

### 4b. Catch dev up to `174` (fixes the crash loop)

Dev must apply `170, 171, 173, 174` (and re-apply `164`'s effect). Two options:

**Option A (preferred) — let goose do it with `-allow-missing`.** This executes each migration
for real (so `00170`'s built-in `indisvalid` guard and the idempotent drop-then-create bodies
apply) instead of hand-marking versions:

```bash
# in the dev CT, migrations are at /app/migrations inside the image
goose -dir <migrations-dir> -allow-missing postgres "$DSN" up
```

Applied set after: `164, 170, 171, 173, 174` (plus already-applied `165`, `172`), reaching
`174`. `-allow-missing` is required because `170`/`171` are older than the recorded `172`.

**Option B (more manual)** — apply the `170`/`171`/`173`/`174` SQL statement-by-statement from
`/app/migrations/` in the image, then `migrate -c mark-applied -v 170 … -v 171 …`, then let
the server start so goose applies `173`/`174`. **Only use `mark-applied` for migrations whose
catalog effect you have just verified** — that is precisely the shortcut that created #734.

**Post-check (both options):** expect 4 valid HNSW indexes and no ivfflat among them:

```sql
SELECT n.nspname, c.relname, am.amname, i.indisvalid
FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_am am ON am.oid=c.relam
WHERE am.amname IN ('hnsw','ivfflat') ORDER BY 2;
-- IDX_graph_objects_embedding_v2_hnsw            | hnsw | t
-- idx_chunks_embedding_hnsw                     | hnsw | t
-- idx_graph_relationships_embedding_hnsw        | hnsw | t
-- idx_skills_embedding_hnsw                     | hnsw | t
-- (no ivfflat rows)
SELECT max(version_id) FROM goose_db_version;  -- 174
```

## 5. Local backlog assessment (`memtest-db`)

`memtest-db` is at goose **155** (last applied 2026-09-17 08:52:24). Unapplied on the way to
`origin/main` `174` — **15 migrations**:

`00156, 00157, 00158, 00159, 00160, 00161, 00162, 00163, 00164, 00165, 00170, 00171, 00172, 00173, 00174`
(note the repo has no `00166`–`00169`).

| migration | class | lock / hazard (on a small local DB) |
|---|---|---|
| 00156 add mcp tool output schema | additive | trivial |
| 00157 backfill extraction_job_id | data + index | small; non-concurrent `CREATE INDEX` (brief `SHARE`) |
| 00158 builtin graph schemas unique | **data mutation** | dedupes/repoints FK rows; deletions of duplicate builtin schemas |
| 00159 allow imported backups | DDL | `DROP CONSTRAINT` (brief `ACCESS EXCLUSIVE`) + nullable column |
| 00160 agent share links | additive | creates tables/indexes |
| 00161/00162/00163 | index-only, `CONCURRENTLY` | `NO TRANSACTION`; 00163 also `ANALYZE` |
| **00164** graph_objects HNSW | index swap, `CONCURRENTLY` | `NO TRANSACTION`; **no post-condition guard** (this is the #734 defect) |
| 00165 normalize project roles | data `UPDATE` | trivial |
| 00170 chunks/skills HNSW | index swap, `CONCURRENTLY` | `NO TRANSACTION`; has an `indisvalid` DO-block guard |
| 00171 graph_relationships HNSW | index swap, `CONCURRENTLY` | `NO TRANSACTION` |
| 00172 graph_relationships namespace | DDL + index + backfill | `ADD COLUMN` + non-concurrent partial index + `UPDATE` |
| 00173 email_jobs started_at | additive | `ADD COLUMN`, trivial |
| 00174 graph_objects FTS identifiers | function + backfill + `CONCURRENTLY` GIN | `NO TRANSACTION`; longest-running step |

**Catch-up command (do NOT run on the shared DB without approval):**

```bash
# repo root
task migrate:up
# or, from apps/server
POSTGRES_PASSWORD=… go run ./cmd/migrate -c up
# or explicit DSN against the shared DB
DATABASE_URL='postgres://emergent:…@localhost:5432/emergent?sslmode=disable' go run ./cmd/migrate -c up
```

Because all 15 are pending at once, goose applies them in numeric order
(`…165, 170, 171, 172, 173, 174`) in a **single run** — the out-of-order guard that breaks dev
does **not** apply locally. Local data volumes are small, so the "long-locking" steps are
sub-second here.

**Chain verified on a scratch container** (own container, `pgvector/pgvector:pg16`, matching
dev's PG16): full `00001 → 174` applied cleanly via `go run ./cmd/migrate -c up`, finishing:

```
OK   00164_hnsw_graph_objects_embedding.sql (5.08ms)
OK   00170_embedding_indexes_hnsw.sql (14.62ms)
OK   00171_graph_relationships_embedding_hnsw.sql (4.68ms)
OK   00172_graph_relationships_namespace.sql (3.02ms)
OK   00173_add_email_jobs_started_at.sql (1.23ms)
OK   00174_graph_objects_fts_identifiers.sql (6.22ms)
goose: successfully migrated database to version: 174
```

Post-state on scratch: `max(version_id)=174`, four valid HNSW indexes, `0` invalid indexes.

## 6. Durable fixes (proposed)

1. **Catalog post-condition guard for `CONCURRENTLY` index migrations.** `00170` already
   demonstrates the pattern (a `DO $$` block that raises unless the new HNSW index is
   `indisvalid AND indisready` and the old index is gone). `00164` predates it and has no
   guard. Applied migrations are **immutable** (CI "Migration Immutability"), so `00164` must
   not be edited. Proposal: a **new follow-up migration** (e.g. `00175_assert_hnsw_embedding_indexes.sql`)
   that asserts the invariant — all four HNSW indexes valid, no ivfflat — and raises loudly.
   It must be merged only **after** dev is remediated (otherwise it would fail the dev deploy).
2. **Adopt the guard as a family convention** for every `-- +goose NO TRANSACTION` migration
   that does `CREATE INDEX CONCURRENTLY`; ideally enforce with a CI lint that flags
   `NO TRANSACTION` + `CONCURRENTLY` migrations lacking an `indisvalid` assertion.
3. **Block/verify `mark-applied`.** `migrate -c mark-applied` / `Migrator.MarkApplied` can
   record any version with no catalog verification — the exact mechanism behind #734. At
   minimum, refuse (or require an extra flag) for versions whose migration touches indexes,
   and log the actor.
4. **Reject out-of-order migration version merges.** `00172` merged 25 min before `00170`/
   `00171`, which is what put dev into the crash loop. A CI check should reject a PR whose new
   migration version is ≤ the highest version already on the base branch (or require
   renumbering), so goose's missing-migration guard can never fire on a deploy.

## 7. Read-only diagnostics (safe to re-run any time)

```sql
-- goose bookkeeping
SELECT version_id, is_applied, tstamp FROM goose_db_version ORDER BY version_id;

-- ANN index catalog truth
SELECT n.nspname, c.relname, am.amname, i.indisvalid, i.indisready,
       pg_size_pretty(pg_relation_size(c.oid)) AS size
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_am am ON am.oid = c.relam
WHERE c.relname ILIKE '%embedding%' OR am.amname IN ('hnsw','ivfflat')
ORDER BY 1,2;

-- any leftover partial-building indexes
SELECT count(*) FROM pg_index WHERE NOT indisvalid OR NOT indisready;
```

```bash
# was a CONCURRENTLY build killed? (errors are logged even with log_statement=none)
ssh emergent-dev "docker logs --since 24h emergent-dev-memory-db-1 2>&1 | grep -Ei 'FATAL|ERROR|CONCURRENTLY'"
ssh emergent-dev "docker logs --tail 40 emergent-dev-memory-server-1 | grep -i migrat"
```
