# Spec Server 2

Minimal ingestion server aligned with the spec:

- Ingest a URL or uploaded file, extract text, chunk, embed with Google Gemini `text-embedding-004`.
- Store in Postgres with pgvector and FTS.

## Technologies

- **Backend:** Go (1.25+)
- **Database:** PostgreSQL 16 with pgvector
- **Authentication:** Zitadel (OIDC)
- **CLI:** Go (emergent-cli)
- **Automation:** Taskfile

## Quick Installation

### One-Line Install (Recommended)

Install server + CLI in 2-3 minutes with pre-built images:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent/main/deploy/minimal/install-online.sh | bash
```

### Self-Update

```bash
emergent upgrade
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
- **Langfuse**: For tracing LLM extraction jobs.
- **Grafana Tempo**: For OpenTelemetry tracing (opt-in via `OTEL_ENABLED=true`).

## Production Deployment

Spec Server 2 supports production deployment with Docker Compose.

### Architecture

**Production Stack:**
- **Backend**: Go (Single binary)
- **Database**: PostgreSQL 16 with pgvector extension
- **Auth**: Zitadel (self-hosted or cloud IAM)

See `docker-compose.staging.yml` for a production-ready template.

## Documentation

All project documentation is located in the `/docs` directory:

- **/docs/setup**: Guides for setting up the project and its dependencies.
- **/docs/guides**: How-to guides and quick references for developers.
- **/docs/features**: Detailed documentation on specific features.
- **/docs/technical**: Deep dives into the architecture and technical implementation details.
- **/docs/database**: Database schema documentation.
