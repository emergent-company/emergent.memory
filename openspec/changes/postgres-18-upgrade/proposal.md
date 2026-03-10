## Why

PostgreSQL 18 is the next major release and `pgvector/pgvector:pg18` is already available (0.8.2-pg18). Upgrading keeps the platform on a supported, current major version, avoids accumulating a two-version gap, and picks up PG18 performance and SQL improvements. The upgrade infrastructure already exists from the pg16→pg17 migration; this change wires it up for pg17→pg18.

## What Changes

- `pgvector/pgvector:pg17` → `pgvector/pgvector:pg18` in all Docker Compose files and Dockerfiles
- `pgUpgradeImage` constant updated to `pgautoupgrade/pgautoupgrade:18-bookworm` (the automated in-place upgrade image)
- `PostgresMajorVersion` constant updated from `17` → `18` (single source of truth that drives all upgrade logic)
- `PostgresImage` constant updated to `pgvector/pgvector:pg18`
- `postgresql16-client` in `deploy/self-hosted/Dockerfile.server` updated to `postgresql17-client` (Alpine 3.21 ships pg17 client; pg18 client not yet packaged — **see design**)
- `install-online.sh` inline compose template updated from `pg17` → `pg18`
- All test fixtures updated to reflect new target version
- Success message in `RunPostgresUpgrade` updated to say "PostgreSQL 18"

## Capabilities

### New Capabilities
- `postgres-18-upgrade`: Automatic in-place upgrade of existing pg17 installations to pg18 during `memory server upgrade`, using the `pgautoupgrade` container approach already proven for pg16→pg17.

### Modified Capabilities
- `deployment`: Postgres image version bumped across all deployment artifacts.

## Impact

**Code files changed:**
- `tools/cli/internal/installer/pg_upgrade.go` — constants `pgUpgradeImage`, success message
- `tools/cli/internal/installer/templates.go` — constants `PostgresImage`, `PostgresMajorVersion`
- `deploy/self-hosted/Dockerfile.server` — `postgresql16-client` → `postgresql17-client`
- `deploy/self-hosted/docker-compose.yml` — db image
- `deploy/self-hosted/docker-compose.local.yml` — db image
- `docker/Dockerfile.postgres` — base image
- `docker/docker-compose.e2e.yml` — db image
- `docker/e2e/docker-compose.yml` — db image
- `deploy/self-hosted/install-online.sh` — inline pg17 image reference
- `tools/cli/internal/installer/docker_test.go` — expected image string
- `tools/cli/internal/installer/installer_test.go` — expected image string

**No Go driver changes required.** pgx/v5 and bun are protocol-level and version-agnostic.

**No migration changes required.** Existing SQL migrations are compatible with PG18.

**No schema changes.** pgvector ivfflat/HNSW indexes and `vector(768)` type are supported in pg18.
