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

## Repository Layout

This is a monorepo:

- `apps/server` — Go server (Echo + Bun ORM + fx, Zitadel auth)
- `apps/web-ui` — Web UI (Go templ + HTMX gateway)
- `apps/cli` — `memory` CLI
- `apps/connector.linux` — Linux connector
- `apps/connector.mac` — macOS connector
- `apps/ios` — iOS app

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

### After Installation

Initialize a project to start using Memory:

```bash
memory init
```

### Self-Update

```bash
memory upgrade
```

## Schema-Aware Chat (MCP Integration)

The chat system integrates with the Model Context Protocol (MCP) to provide intelligent, real-time schema information. When users ask questions about the database schema (version, changes, type definitions), the system automatically:

1. **Detects schema queries** using pattern matching
2. **Queries the database** via MCP tools (schema_version, schema_changelog, type_info)
3. **Injects context** into LLM prompts
4. **Streams responses** with accurate, up-to-date schema information

## Observability

The system supports integration with:
- **Grafana Tempo**: For OpenTelemetry tracing (opt-in via `OTEL_EXPORTER_OTLP_ENDPOINT`).

## Documentation

Published documentation is available at **https://emergent-company.github.io/emergent.memory/**.

Development, deployment and environment documentation is maintained in a separate repository.

The published documentation lives in [`docs/site`](docs/site) and is built with
`mkdocs.yml`. The database schema is generated from the migrations into
[`docs/schema.dbml`](docs/schema.dbml).

## License

This project is licensed under the [MIT License](LICENSE) (© 2024-2026 Emergent Company).

Third-party components are attributed separately in [NOTICE](NOTICE).
