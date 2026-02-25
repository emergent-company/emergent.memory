## ADDED Requirements

### Requirement: Dump script connects to Cloud SQL via Auth Proxy

The system SHALL provide a shell script (`scripts/dump-twentyfirst-db.sh`) that authenticates via `gcloud`, downloads the Cloud SQL Auth Proxy binary if not present, starts the proxy as a background tunnel to `twentyfirst-io:us-central1:dep-database-dev-b3db59nu`, runs `pg_dump --format=custom` through the tunnel, and exports selected tables as `.csv.gz` files to `/tmp/twentyfirst_dump/`.

#### Scenario: Successful dump with active gcloud credentials

- **WHEN** the developer runs `scripts/dump-twentyfirst-db.sh` with an active `gcloud auth` session
- **THEN** the script starts the Auth Proxy, runs `pg_dump`, writes `/tmp/twentyfirst_dump/twentyfirst-db.dump`, exports tables as `.csv.gz` files, kills the proxy, and exits 0

#### Scenario: No active gcloud credentials

- **WHEN** the developer runs the script without an authenticated gcloud account
- **THEN** the script SHALL print a clear error message with `gcloud auth login` instructions and exit non-zero before attempting any connection

#### Scenario: Auth Proxy binary already cached

- **WHEN** the Cloud SQL Auth Proxy binary already exists at `/tmp/cloud-sql-proxy`
- **THEN** the script SHALL skip the download and reuse the cached binary

#### Scenario: Schema introspection on first run

- **WHEN** the dump script runs successfully
- **THEN** it SHALL print a list of all tables found in the database to stdout so the developer can identify which tables to map

---

### Requirement: Seeder is a standalone Go script

The system SHALL provide a standalone Go program at `scripts/seed-twentyfirst-db/main.go` with its own `go.mod` (module name `seed-twentyfirst-db`), depending only on Go stdlib and `github.com/lib/pq` for Postgres connectivity. It SHALL NOT import any package from `apps/server-go` or the emergent SDK.

#### Scenario: Run without server-go dependency

- **WHEN** a developer runs `go run scripts/seed-twentyfirst-db/main.go` from any directory
- **THEN** the script SHALL compile and run without requiring the `apps/server-go` module or any internal emergent package

#### Scenario: go.mod is self-contained

- **WHEN** the developer inspects `scripts/seed-twentyfirst-db/go.mod`
- **THEN** it SHALL declare its own module name and list only external dependencies (stdlib, `lib/pq`)

---

### Requirement: Seeder reads CSV exports and loads via emergent graph API

The seeder SHALL read the `.csv.gz` files produced by the dump script, map rows to emergent graph objects and relationships, and POST them to the emergent graph API using the `/api/graph/objects/bulk` and `/api/graph/relationships/bulk` endpoints in batches of 100 with up to 20 concurrent goroutines.

#### Scenario: Successful object load

- **WHEN** the seeder runs against valid `.csv.gz` files and a reachable emergent server
- **THEN** it SHALL POST all rows as graph objects in batches, log progress per batch, and transition phase state from `objects_pending` to `objects_done`

#### Scenario: Successful relationship load

- **WHEN** the objects phase is complete and a relationship mapping is defined
- **THEN** it SHALL POST all relationships in batches and transition to `done`

#### Scenario: Partial failure is recoverable

- **WHEN** a batch fails (network error or 5xx response)
- **THEN** the seeder SHALL log the failure to `rels_failed.jsonl`, continue with remaining batches, and exit non-zero with a summary of failed batches

---

### Requirement: Dry-run and limit modes

The seeder SHALL support `DRY_RUN=true` (process only first 100 rows, skip API calls, print what would be sent) and `SEED_LIMIT=<n>` (process at most N rows) environment variables.

#### Scenario: DRY_RUN mode

- **WHEN** `DRY_RUN=true` is set
- **THEN** the seeder SHALL print the first 100 objects/relationships that would be sent but SHALL NOT make any HTTP calls to the emergent API

#### Scenario: SEED_LIMIT mode

- **WHEN** `SEED_LIMIT=500` is set
- **THEN** the seeder SHALL process at most 500 rows and then stop, loading them normally via the API

---

### Requirement: Checkpoint and resume

The seeder SHALL persist state to `/tmp/twentyfirst_seed_state/` (files: `state.json`, `idmap.json`, `rels_done.txt`, `rels_failed.jsonl`) so that an interrupted run can be resumed from where it left off by re-running the same command.

#### Scenario: Resume after interruption

- **WHEN** the seeder is interrupted (SIGINT or crash) mid-run and re-invoked
- **THEN** it SHALL read `state.json` to determine the current phase, skip already-completed batches listed in `rels_done.txt`, and continue from where it stopped

#### Scenario: SIGINT graceful shutdown

- **WHEN** the developer presses Ctrl+C during a run
- **THEN** the seeder SHALL finish the in-flight batch, save state, and exit cleanly

---

### Requirement: Convenience shell script for seeder invocation

The system SHALL provide `scripts/seed-twentyfirst-db.sh` that exports required env vars (`SERVER_URL`, `API_KEY`, `PROJECT_ID`) and invokes the standalone Go script via `go run`, analogous to `docs/tests/imdb_test_script.sh`.

#### Scenario: Developer runs the convenience script

- **WHEN** the developer runs `scripts/seed-twentyfirst-db.sh`
- **THEN** it SHALL export the env vars and invoke `go run scripts/seed-twentyfirst-db/main.go`
