# Memory

Knowledge graph platform for ingesting, storing, and querying structured knowledge:

- Ingest URLs or uploaded files, extract text, chunk, and embed.
- Store in Postgres with pgvector and FTS.

## Technologies

- **Backend:** Go (1.25+)
- **Database:** PostgreSQL 16 with pgvector
- **Authentication:** Zitadel (OIDC)
- **CLI:** Go (memory-cli)
- **Automation:** Taskfile

## Quick Installation

### Server + CLI (Recommended)

Install server + CLI in 2-3 minutes with pre-built images:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/install-online.sh | bash
```

### CLI only

Connect to an existing Memory server without installing the full stack:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/install-cli.sh | bash
```

### Self-Update

```bash
memory upgrade
```

## Getting Started

- **[Environment Setup Guide](docs/guides/ENVIRONMENT_SETUP.md)** - Comprehensive guide for local, dev, staging, and production environments.
- **[Runbook](RUNBOOK.md)** - Operational details and daily workflows.

## Key Commands

The project uses `task` (Taskfile) for automation:

```bash
# Build server
task build

# Run unit tests
task test

# Run integration tests
task test:integration

# Run server with hot reload (requires air)
task dev

# Database migrations
task migrate:up
task migrate:status
```

## Schema-Aware Chat (MCP Integration)

The chat system integrates with the Model Context Protocol (MCP) to provide intelligent, real-time schema information. When users ask questions about the database schema (version, changes, type definitions), the system automatically:

1. **Detects schema queries** using pattern matching
2. **Queries the database** via MCP tools (schema_version, schema_changelog, type_info)
3. **Injects context** into LLM prompts
4. **Streams responses** with accurate, up-to-date schema information

## Observability

The system supports integration with:
- **Grafana Tempo**: For OpenTelemetry tracing (opt-in via `OTEL_ENABLED=true`).

## Production Deployment

Memory supports production deployment with Docker Compose.

### Architecture

**Production Stack:**
- **Backend**: Go (Single binary)
- **Database**: PostgreSQL 16 with pgvector extension
- **Auth**: Zitadel (self-hosted or cloud IAM)

See `docker-compose.staging.yml` for a production-ready template.

## Documentation

Published documentation is available at **https://emergent-company.github.io/emergent.memory/**.

Additional in-repo documentation is located in the `/docs` directory:

- **/docs/setup**: Guides for setting up the project and its dependencies.
- **/docs/guides**: How-to guides and quick references for developers.
- **/docs/features**: Detailed documentation on specific features.
- **/docs/technical**: Deep dives into the architecture and technical implementation details.
- **/docs/database**: Database schema documentation.
