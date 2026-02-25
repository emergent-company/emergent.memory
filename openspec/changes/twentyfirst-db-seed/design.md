## Context

The `twentyfirst-io` GCP project runs a Cloud SQL PostgreSQL instance (`dep-database-dev-b3db59nu`). We need a two-phase workflow:

1. **Dump phase** — extract data from Cloud SQL to a local `.dump` file using `pg_dump` via a Cloud SQL Auth Proxy tunnel
2. **Seed phase** — read the dump, map the source schema to emergent graph objects/relationships, and load via the emergent graph API (bulk endpoints)

The IMDB seeder (`apps/server-go/cmd/seed-imdb/main.go`) is the established pattern for this. It uses the emergent Go SDK, batched bulk API calls (100 records/batch, 20 concurrent goroutines), a checkpoint/resume system, and SIGINT-safe graceful shutdown. The twentyfirst-db seeder will follow this pattern exactly.

The Cloud SQL Auth Proxy is the recommended secure way to connect to Cloud SQL without exposing the instance publicly. It is not currently installed; the dump script will download it at runtime.

## Goals / Non-Goals

**Goals:**

- A repeatable, one-command dump script for the `twentyfirst-io` Cloud SQL database
- A Go seeder command that reads the dump and loads data into any emergent environment via the graph API
- Dry-run mode (limited row count) for fast iteration
- Resumable state so a partial seed can be continued after interruption
- Follows existing IMDB seeder conventions exactly (same env vars, same state dir pattern, same SDK usage)

**Non-Goals:**

- Syncing the databases in real-time or on a schedule
- Migrating the Cloud SQL schema into the emergent Postgres schema directly (no raw SQL restore)
- Supporting databases other than `dep-database-dev-b3db59nu`
- Any UI or admin interface for triggering the seed

## Decisions

### Decision 1: Cloud SQL Auth Proxy for dump connectivity

**Choice**: Download `cloud-sql-proxy` binary at runtime in the dump script, start it as a background process, run `pg_dump` against the local tunnel, then kill the proxy.

**Rationale**: The Auth Proxy is the only Cloud SQL connection method that works without adding authorized networks. It uses gcloud IAM credentials (already available via `gcloud auth`), requires no firewall changes, and is the officially recommended approach. The binary is a single static download (~30 MB), so embedding the download in the script keeps setup friction near zero.

**Alternative considered**: `gcloud sql export sql` to a GCS bucket, then `gsutil cp` to download. Rejected because it requires a GCS bucket, IAM write permissions, and an extra download step, and produces a `mysqldump`-style SQL file that needs more parsing.

### Decision 2: `pg_dump` custom format (`.dump`)

**Choice**: Use `pg_dump --format=custom` producing a binary `.dump` file stored at `/tmp/twentyfirst-db.dump`.

**Rationale**: Custom format is compressed, supports parallel restore, and can be selectively restored by table/schema. Plain SQL dumps are larger and harder to introspect. The `.dump` file is ephemeral (not committed to git).

### Decision 3: Seeder reads dump via `pg_restore --list` + direct table reads

**Choice**: The Go seeder does NOT use `pg_restore`. Instead the dump script also exports selected tables as CSV (using `COPY TO STDOUT`) so the Go seeder can stream them directly.

**Rationale**: The Go seeder needs to iterate rows in Go, not replay SQL. Having the dump script export key tables as `.csv.gz` files alongside the `.dump` gives the seeder a simple streaming interface identical to how the IMDB seeder reads `.tsv.gz` files. This also avoids needing a restore target database.

### Decision 4: Schema/table selection at dump time

**Choice**: The dump script will dump ALL tables by default (let the schema be discovered at runtime after auth). Post-auth we inspect the schema and export relevant tables as CSV. If the schema is unknown at script-write time, the script includes a `--table` flag to specify tables interactively.

**Rationale**: We don't yet know the exact schema of `dep-database-dev-b3db59nu`. The dump script will introspect it on first run and output a schema summary. The seeder's table→object-type mapping will be configured via constants in `main.go` once the schema is known.

### Decision 5: Standalone Go script, not part of server-go module

**Choice**: The seeder lives as a self-contained Go script at `scripts/seed-twentyfirst-db/main.go` with its own `go.mod`, completely independent of `apps/server-go`. It calls the emergent REST API directly via `net/http` — no internal SDK import.

**Rationale**: The user explicitly requested an independent script. Keeping it outside `apps/server-go` means it can be run by anyone with only `go` and `curl`-style HTTP access to the emergent API, with no need to clone or understand the server codebase. Dependencies are minimal (stdlib only, or optionally `lib/pq` for reading the dump via a local Postgres restore).

**Alternative considered**: Embedding in `apps/server-go/cmd/` like the IMDB seeder. Rejected — that couples the script to the server module's full dependency graph and build system.

### Decision 6: State dir and env var conventions (adapted for standalone)

**Choice**: Same env vars (`SERVER_URL`, `API_KEY`, `PROJECT_ID`, `DRY_RUN`, `SEED_LIMIT`) and same checkpoint state dir pattern (`/tmp/twentyfirst_seed_state/`) as the IMDB seeder, but implemented with stdlib only.

**Rationale**: Consistency with the IMDB seeder mental model, without the SDK coupling.

## Risks / Trade-offs

- **Unknown source schema** → The seeder's object/relationship mapping cannot be fully specified until the dump is successfully retrieved and inspected. The tasks artifact will include a placeholder step for this. Mitigation: dump script outputs a `\dt` schema listing before exporting.
- **gcloud auth required at runtime** → The dump script will fail if no gcloud credentials are active. Mitigation: script checks `gcloud auth list` upfront and prints a clear error with `gcloud auth login` instructions.
- **Cloud SQL Auth Proxy version** → The script pins a specific proxy version to avoid unexpected breakage. Mitigation: pin version in the script constant, document how to update it.
- **Large dump size** → If the database is large, the dump and CSV files in `/tmp` may consume significant disk space. Mitigation: script prints estimated size before downloading; seeder streams CSVs rather than loading into memory.
- **Data sensitivity** → The `twentyfirst-io` database may contain real user data. Mitigation: dump files written to `/tmp` (not committed), script warns that output may contain PII.

## Migration Plan

1. Developer runs `scripts/dump-twentyfirst-db.sh` (requires active `gcloud auth`)
2. CSV files land in `/tmp/twentyfirst_dump/`
3. Developer runs `scripts/seed-twentyfirst-db.sh` (or `go run ./cmd/seed-twentyfirst-db`) pointing at their target emergent instance
4. Seeder loads objects then relationships, with resume support
5. No rollback needed — seed data can be cleared by deleting the project in emergent admin

## Open Questions

- What is the exact database name inside the Cloud SQL instance? (default `postgres` assumed; will be confirmed at first dump run)
- What Cloud SQL user should be used for the dump? (`postgres` assumed)
- Which tables contain the primary entities we want as graph objects? (to be discovered from schema introspect on first run)
- Should the dump script be added to `.gitignore` output paths, or is `/tmp` sufficient? (`/tmp` assumed sufficient)
