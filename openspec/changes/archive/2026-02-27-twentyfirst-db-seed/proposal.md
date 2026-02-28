## Why

The `twentyfirst-io` GCP project hosts a Cloud SQL PostgreSQL database (`dep-database-dev-b3db59nu`) with real-world data that we want available in the emergent dev environment as seed data for testing and development. Without a repeatable dump-and-load workflow, developers cannot easily reproduce this data locally or in dev.

## What Changes

- New shell script `scripts/dump-twentyfirst-db.sh` — authenticates via `gcloud`, installs Cloud SQL Auth Proxy if needed, tunnels to the Cloud SQL instance, and runs `pg_dump` to produce a `.dump` file
- New Go seed command `apps/server-go/cmd/seed-twentyfirst-db/main.go` — reads the dump, transforms the data into emergent graph objects and relationships, and loads them via the emergent graph API (bulk endpoints), following the same pattern as `cmd/seed-imdb`
- New convenience shell script `scripts/seed-twentyfirst-db.sh` — sets env vars and invokes the Go seeder, analogous to `docs/tests/imdb_test_script.sh`

## Capabilities

### New Capabilities

- `twentyfirst-db-seed`: Dump the `twentyfirst-io` Cloud SQL database and load it into the emergent project as seed data via the graph API, with dry-run support, resumable state, and a companion dump script

### Modified Capabilities

_(none — no existing spec-level behavior changes)_

## Impact

- **New files**: `scripts/dump-twentyfirst-db.sh`, `scripts/seed-twentyfirst-db.sh`, `apps/server-go/cmd/seed-twentyfirst-db/main.go`
- **Dependencies**: `gcloud` CLI (already installed), Cloud SQL Auth Proxy binary (downloaded at runtime), `pg_dump` (already installed at `/usr/sbin/pg_dump`)
- **GCP project**: `twentyfirst-io`, instance `dep-database-dev-b3db59nu`
- **Target**: emergent graph API (`/api/graph/objects/bulk`, `/api/graph/relationships/bulk`) — no schema or API changes required
- **No breaking changes**
