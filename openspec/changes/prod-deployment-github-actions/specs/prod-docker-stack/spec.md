## ADDED Requirements

### Requirement: Multi-stage Dockerfile produces a minimal production image
The infra repo SHALL contain a `Dockerfile` that builds the Emergent Go server using a multi-stage build: a builder stage compiles the binary, and a minimal runtime stage (distroless or Alpine) contains only the binary and CA certificates.

#### Scenario: Image build produces small image
- **WHEN** `docker build -t emergent-server .` is run from the infra repo root
- **THEN** the resulting image SHALL contain the `emergent` server binary and no Go toolchain
- **AND** the image size SHALL be under 100 MB

#### Scenario: Image is tagged with release version
- **WHEN** the build is triggered with `--build-arg VERSION=v1.2.3`
- **THEN** the binary SHALL embed that version string
- **AND** the image SHALL be tagged `ghcr.io/emergent-company/emergent-server:v1.2.3`

### Requirement: Docker Compose file defines the full production stack
The infra repo SHALL contain a `docker-compose.yml` that declares all services required to run Emergent in production: the server, PostgreSQL with pgvector, and MinIO object storage.

#### Scenario: All core services start
- **WHEN** `docker compose up -d` is run on the host
- **THEN** the `server`, `postgres`, and `minio` services SHALL all reach a healthy state within 60 seconds
- **AND** no service SHALL bind to a public-facing port directly (all traffic goes through the reverse proxy)

#### Scenario: Server is reachable on internal port
- **WHEN** the stack is running
- **THEN** the server SHALL listen on `127.0.0.1:3012` (or the configured internal port)
- **AND** the reverse proxy SHALL be able to forward requests to that port

#### Scenario: Postgres uses pgvector image
- **WHEN** the stack starts
- **THEN** the `postgres` service SHALL use the `pgvector/pgvector:pg16` image
- **AND** the `vector` extension SHALL be available in the database

#### Scenario: Environment variables loaded from .env file
- **WHEN** the stack starts with a `.env` file present in the same directory
- **THEN** all service containers SHALL receive the environment variables defined in `.env`
- **AND** no secrets SHALL be hardcoded in `docker-compose.yml`

### Requirement: Migration service runs Goose migrations
The Docker Compose file SHALL include a `migrator` service that runs database migrations using Goose and then exits.

#### Scenario: Migrations run before server starts
- **WHEN** `docker compose run --rm migrator` is executed
- **THEN** all pending Goose migrations SHALL be applied to the production database
- **AND** the container SHALL exit with code 0 on success

#### Scenario: Migration failure is visible
- **WHEN** a migration fails
- **THEN** the migrator container SHALL exit with a non-zero exit code
- **AND** the error message SHALL appear in the container logs

### Requirement: Named volumes persist data across restarts
The Docker Compose file SHALL use named Docker volumes for Postgres data and MinIO data so that data survives container restarts and image updates.

#### Scenario: Data survives server update
- **WHEN** `docker compose up -d` is re-run with a new server image
- **THEN** Postgres and MinIO named volumes SHALL be preserved
- **AND** no data SHALL be lost from the previous run

### Requirement: Health check defined for server service
The server service in `docker-compose.yml` SHALL have a Docker health check that polls the `/health` endpoint.

#### Scenario: Compose reports server healthy
- **WHEN** the server starts and `/health` returns HTTP 200
- **THEN** `docker compose ps` SHALL show the server service status as `healthy`

#### Scenario: Compose reports server unhealthy
- **WHEN** the server fails to start and `/health` does not return HTTP 200 within the configured interval
- **THEN** `docker compose ps` SHALL show the server service status as `unhealthy`
