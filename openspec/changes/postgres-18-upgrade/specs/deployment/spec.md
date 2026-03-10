## ADDED Requirements

### Requirement: PostgreSQL 18 is the target database version for all deployments
All deployment artifacts SHALL reference PostgreSQL 18 (`pgvector/pgvector:pg18`) as the target database image. No deployment artifact SHALL reference pg17 or earlier as the intended runtime version.

#### Scenario: New self-hosted install uses pg18
- **WHEN** a user runs `memory install` using the latest installer
- **THEN** the generated `docker-compose.yml` SHALL use `pgvector/pgvector:pg18` as the db image
- **AND** the database container SHALL start successfully with pgvector extension available

#### Scenario: E2E tests run against pg18
- **WHEN** the `docker/docker-compose.e2e.yml` is used to start the test environment
- **THEN** the db service SHALL use `pgvector/pgvector:pg18`
- **AND** all existing migrations and queries SHALL pass without modification

#### Scenario: Developer local environment uses pg18
- **WHEN** a developer runs `docker compose -f deploy/self-hosted/docker-compose.local.yml up`
- **THEN** the db service SHALL use `pgvector/pgvector:pg18`

### Requirement: Server container runtime uses Alpine 3.23
The runtime stage of `deploy/self-hosted/Dockerfile.server` SHALL use `alpine:3.23` as the base image and install `postgresql18-client` for pg-compatible client tools (`pg_isready`, `pg_dump`).

#### Scenario: pg_isready works in server container
- **WHEN** Docker health-check calls `pg_isready` inside the server container
- **THEN** the command SHALL be present and SHALL connect to the pg18 database without version warnings

#### Scenario: pg_dump is available in server container
- **WHEN** an operator runs `docker exec memory-server pg_dump ...`
- **THEN** the pg18-compatible `pg_dump` binary SHALL be present and SHALL dump from a pg18 database without warnings
