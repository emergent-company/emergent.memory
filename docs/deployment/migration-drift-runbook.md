# Runbook — goose migration drift (recorded-but-no-op migrations and version gaps)

Operational guide for two independent failure modes where the goose bookkeeping in
`public.goose_db_version` disagrees with the database catalog:

- **Mode A — recorded-but-no-op migration:** a version is recorded as applied even
  though its effects never landed (issue **#734**).
- **Mode B — version gap:** a migration is merged out of version order, leaving a
  lower-numbered gap that makes goose refuse to start the server (issue **#750**).

Both are detection + repair procedures for any deployment (dev included), not a record
that any particular environment has been repaired. Durable prevention lives in **#750**;
this runbook cross-references it rather than duplicating it.

> **Provenance / honesty note.** The `#734` and `#750` facts below are taken from those
> issue reports and the dev inspection done when they were filed — cited, not
> independently re-verified here. No step in this document is claimed to have been run on
> dev. The few results that were exercised during authoring ran on a **throwaway
> `pgvector/pgvector:pg16` container, never on dev**, and are labelled as such.

## 0. Environment facts

- **Remote dev:** host `emergent-dev`; containers `emergent-dev-memory-server-1`,
  `emergent-dev-memory-db-1` (pg16 / pgvector, db `emergent`).
- **Server image:** `ghcr.io/emergent-company/memory-server:dev` (mutable tag).
- **Migrations run at server-container startup** via `/usr/local/bin/emergent-migrate -c up`
  in `deploy/self-hosted/entrypoint.sh`; the binary is built from `apps/server/cmd/migrate`.
  There is **no separate `migrator` service** in the dev compose file, so the
  `memory-dev-mcp.migrate_db` tool (which would run `docker compose run --rm migrator`)
  has no such service on dev.
- **Local shared DB:** `memtest-db` (`localhost:5432`, db `emergent`), shared with other
  sessions — do not mutate without approval. Per **#734**, local `memtest-db` being behind
  is *not* evidence of remote-dev drift; do not conflate the two.

## 1. Mode A — recorded-but-no-op migration (#734)

**Symptom as reported in #734:** on dev, `goose_db_version` recorded `00164`
(`00164_hnsw_graph_objects_embedding.sql`) as applied, but the `kb` catalog contained **no
HNSW index at all** and the legacy `kb."IDX_graph_objects_embedding_v2_ivfflat"` was still
present. So a goose row existed without the migration's effects.

**Detect it — compare the catalog against the recorded versions:**

```sql
-- ANN index catalog truth (schema-qualified, read-only)
SELECT n.nspname, c.relname, am.amname, i.indisvalid, i.indisready,
       pg_size_pretty(pg_relation_size(c.oid)) AS size
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_am am ON am.oid = c.relam
WHERE am.amname IN ('hnsw', 'ivfflat')
ORDER BY 1, 2;
```

A recorded-but-no-op **index swap** shows the *old* access method still present and the
*new* one absent — e.g. `ivfflat` present and **zero `hnsw` rows** — even though
`goose_db_version` lists the version applied. Check `indisvalid`/`indisready` too: an
interrupted `CONCURRENTLY` build leaves an **INVALID** index, which is the same
"effects not really live" hazard in a different shape.

## 2. Mode B — version gap hard-blocks startup (#750)

**Symptom as reported in #750:** the dev server crash-looped (`RestartCount=15`,
`/health` unreachable) with:

```
Error running migrations: error: found 2 missing migrations before current version 172:
	version 170: 00170_embedding_indexes_hnsw.sql
	version 171: 00171_graph_relationships_embedding_hnsw.sql
```

`00172` (`rel-search-namespace-denorm`) had been merged and applied **before** the
lower-numbered `00170`/`00171`. Once a higher version is recorded, goose's default ordering
treats a lower-numbered missing migration as fatal and the server will not boot.

**Detect it — find a gap below the maximum recorded version:**

```sql
SELECT version_id, is_applied, tstamp
FROM public.goose_db_version
ORDER BY version_id;
```

Cross-check the result against the migration files shipped in the image
(`/app/migrations/`, embedded in the runner). A version **below `max(version_id)` that has
a migration file but no row here is a gap**, and startup will fail until it is resolved.

## 3. Why a row can exist without the effects

goose `v3.26.0`'s `-- +goose NO TRANSACTION` path runs each statement with `ExecContext`,
returns on the **first** error, and only then records the version
(`github.com/pressly/goose/v3@v3.26.0/migration_sql.go`). A `CREATE INDEX CONCURRENTLY`
that is terminated therefore returns an error and is **never recorded**. Per **#734**/*#750*,
the dev `00164` row was accompanied by:

- the legacy ivfflat index file's mtime **predating** the goose row's `tstamp`, and
- the `CREATE INDEX CONCURRENTLY … hnsw` statement terminated via
  `pg_terminate_backend` (which surfaces as `terminating connection due to administrator
  command` in the DB log).

So a persisted version for an un-executed (or partially executed) migration implies the row
was written **out-of-band** — the repo's own non-executing path
`emergent-migrate -c mark-applied -v N` / `Migrator.MarkApplied`
(`apps/server/internal/migrate/migrate.go`), or an equivalent manual
`INSERT INTO goose_db_version`. The *exact* writer is not provable (the DB ran with
`log_statement=none` and the relevant WAL was recycled). Treat any version row that
bypasses `emergent-migrate -c up` as needing catalog verification.

## 4. Remediation

> Everything in this section mutates a database. Run it deliberately, one deployment at a
> time, and never hand-mark a version whose catalog effect you have not just verified —
> that shortcut is exactly what #734 documents.

### 4a. Repair a missing object (Mode A)

Mirror the migration's own body (see
`apps/server/migrations/00164_hnsw_graph_objects_embedding.sql`). Run each statement
**separately / autocommit** — `CONCURRENTLY` cannot run inside a transaction block, which is
why the migration is `-- +goose NO TRANSACTION`.

```sql
-- 64 MB maintenance_work_mem is low for an HNSW build
SET maintenance_work_mem = '512MB';

-- Drop ANY existing relation first. A relation left INVALID by an interrupted build
-- still "exists", so CREATE ... IF NOT EXISTS would skip the rebuild and you would then
-- drop the valid ivfflat below, leaving no usable ANN index. 00164's Up does this too.
DROP INDEX CONCURRENTLY IF EXISTS kb."IDX_graph_objects_embedding_v2_hnsw";

CREATE INDEX CONCURRENTLY IF NOT EXISTS "IDX_graph_objects_embedding_v2_hnsw"
    ON kb.graph_objects USING hnsw (embedding_v2 vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

DROP INDEX CONCURRENTLY IF EXISTS kb."IDX_graph_objects_embedding_v2_ivfflat";
```

**Verify (`indisvalid` must be true and the old index gone):**

```sql
SELECT c.relname, am.amname, i.indisvalid, i.indisready
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_am am ON am.oid = c.relam
WHERE n.nspname = 'kb'
  AND c.relname IN ('IDX_graph_objects_embedding_v2_hnsw',
                    'IDX_graph_objects_embedding_v2_ivfflat');
-- expect exactly one row: hnsw, indisvalid = t, indisready = t; ivfflat row ABSENT
```

**Lock / build notes:**

- `CREATE`/`DROP INDEX CONCURRENTLY` take **`SHARE UPDATE EXCLUSIVE`** on the table: they do
  not block `SELECT/INSERT/UPDATE/DELETE`, but they **do** block other DDL, `VACUUM FULL`,
  `REINDEX`, and any other concurrent build on the same table.
- A terminated build leaves an **INVALID** index; the `DROP … IF EXISTS` above makes a retry
  safe. `pg_terminate_backend` from an outside actor is the realistic killer — stop such
  actors before starting.
- The target column must be populated; an empty/partial vector column builds a valid but
  useless index.

### 4b. Repair a version gap (Mode B)

Apply the missing lower-numbered migrations **with goose's allow-missing option**, so goose
runs each migration for real (their own catalog guards and idempotent drop-then-create
bodies apply) instead of hand-marking versions:

```bash
# migrations are embedded in the runner; the runtime image ships
# /usr/local/bin/emergent-migrate (built from apps/server/cmd/migrate).
emergent-migrate -c up -allow-missing
```

**Runtime requirement — read before relying on this.** `-allow-missing` requires the runner
to pass goose's `goose.WithAllowMissing()` option (goose `v3.26.0`). As of writing, the
stock `apps/server/cmd/migrate/main.go` wires plain `goose.UpContext` and accepts only
`-c/-v`, and the runtime image contains **no standalone `goose` binary**. So either:

- the runner is built with `WithAllowMissing()` (tracked in **#750**), or
- run the pinned goose CLI in a **one-off tool container** against `$DSN`
  (`goose -dir /app/migrations -allow-missing postgres "$DSN" up`), or
- apply the missing bodies' SQL statement-by-statement and only then record them — **only for
  versions whose catalog effect you have just verified** (see the Mode A verification query).

Do **not** reach for `mark-applied` to silence a gap; that is the #734 anti-pattern.

### 4c. Caveat — a migration longer than the deploy interval must run out-of-band

A migration whose runtime **exceeds the deploy/restart interval** gets orphaned every time
the server container is recreated: the in-flight statement is killed mid-transaction, goose
never records the version, and the next boot retries from scratch. Per **#750**, this is
what stranded dev on `00174`'s FTS backfill: over ~108k `kb.graph_objects` rows it takes
roughly **15–30 minutes** and must run uninterrupted, while busy-period deploys recreated
the dev container about every **15 minutes**; it completed only once the merge churn stopped.

Therefore, for any migration expected to run longer than the deploy cadence:

- run it **out-of-band** — a one-off container/runner against the DB while the server is
  stopped, or
- run it inside a **maintenance window** with deploys paused, and keep the runner alive until
  it finishes (no concurrent redeploys, no `pg_terminate_backend`).

### 4d. Post-check (either mode)

```sql
SELECT n.nspname, c.relname, am.amname, i.indisvalid
FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_namespace n ON n.oid = c.relnamespace JOIN pg_am am ON am.oid = c.relam
WHERE am.amname IN ('hnsw', 'ivfflat') ORDER BY 2;
-- expect the four current HNSW embedding indexes, all indisvalid = t, and no ivfflat rows:
--   IDX_graph_objects_embedding_v2_hnsw, idx_chunks_embedding_hnsw,
--   idx_graph_relationships_embedding_hnsw, idx_skills_embedding_hnsw
SELECT max(version_id) FROM goose_db_version;  -- current head of the migration chain
```

## 5. Read-only diagnostics (safe to re-run any time)

```sql
-- goose bookkeeping / gap detection
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

## 6. Prevention — tracked in #750

The durable fixes are **not** repeated here in detail; see **#750** for the full set:

1. **CI merge-order guard** — reject a PR whose new migration number is ≤ the highest
   version already on the base branch, so a merge cannot insert a gap.
2. **Startup gap diagnostics** — on a gap, log the remediation (`-allow-missing` / missing
   filenames), not just the raw goose error.
3. **`mark-applied` / out-of-band verification** — verify (or log) the catalog effect when a
   version is recorded without running it.
4. **Per-migration post-condition guard** — the `indisvalid` `DO $$ … RAISE` pattern already
   present in `apps/server/migrations/00170_embedding_indexes_hnsw.sql` (00164 predates it).
   Applied migrations are immutable, so hardening `00164` means a **new** follow-up migration
   that asserts the invariant, merged only after any target environment is repaired.

Unrelated: `API Tests` flakiness on the live MCP registry fetch is tracked in **#738** and is
not a migration issue.

## 7. Local backlog (`memtest-db`) — local only, not dev

`memtest-db` is simply an un-migrated local DB (see the "Not the same as local dev" note in
**#734**). Because every pending migration is absent at once, goose applies them in numeric
order in a single run — the out-of-order guard that breaks dev does **not** apply locally.
Local volumes are small, so the steps that are slow on dev are sub-second here.

```bash
# repo root — do NOT run against the shared DB without approval
# (POSTGRES_HOST=localhost makes the loopback target explicit; without it the
#  command refuses, see the note below)
POSTGRES_HOST=localhost task migrate:up
# or, from apps/server — -allow-localhost is required for an implicit localhost
POSTGRES_PASSWORD=… go run ./cmd/migrate -c up -allow-localhost
# or an explicit DSN (loopback target is explicit here, so it is allowed)
DATABASE_URL='postgres://emergent:…@localhost:5432/emergent?sslmode=disable' go run ./cmd/migrate -c up
```

> **Target resolution is now fail-closed (issue #754).** `emergent-migrate` prints
> `target: host=… port=… user=… database=… sslmode=… (host source: …)` before connecting, and
> a mutating command (`up`, `up-to`, `down`, `mark-applied`) **refuses** when the resolved host
> is loopback and either a different non-loopback host variable is set
> (`DB_HOST`/`POSTGRES_HOST`/`MEMORY_PG_HOST`), or no host variable was configured at all so
> the target is the built-in `localhost:5432` default. Pass `-allow-localhost` to opt in, set
> `POSTGRES_HOST=localhost` (or `DB_HOST=localhost`) to make the loopback target explicit, or
> set `DATABASE_URL`. Host/port precedence is `DATABASE_URL` > `DB_HOST`/`DB_PORT` >
> `POSTGRES_HOST`/`POSTGRES_PORT` > `MEMORY_PG_HOST`/`MEMORY_PG_PORT` > built-in default.
> Read-only commands (`status`, `version`) are never refused.

The full `00001 → 174` chain was exercised during authoring on a throwaway
`pgvector/pgvector:pg16` container (**not** dev or `memtest-db`) and completed cleanly,
ending at the current migration head. Treat that as a sanity check of the migration chain,
not as a statement about any real database's state.
