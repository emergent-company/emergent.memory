# Memory

**Memory is an AI-native knowledge platform.** Ingest documents, URLs, and
connected data sources; extract entities and relationships into a queryable
knowledge graph; and give agents persistent memory through a CLI, REST API, MCP
server, and SDKs.

## What you can do

- **Ingest** documents, URLs, and third-party sources (Google Drive, Gmail, web
  scrapers) into one store.
- **Extract** structured knowledge — entities, relationships, and schemas — with
  LLM-backed pipelines.
- **Query** the graph with hybrid semantic and keyword search, backed by a
  versioned schema registry.
- **Build agents** with tools, instructions, schedules, and persistent memory,
  then chat with them from the web UI, CLI, or API.
- **Connect** over MCP, the REST API, the Go or Python SDK, or the `memory` CLI.
- **Run it yourself** with the self-hosted installer, or point the CLI at an
  existing server.

## Quickstart

Install the server and CLI on a machine with Docker:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/self-hosted/install-online.sh | bash
```

Create your first project and ask the built-in assistant how to use it:

```bash
memory init
memory ask "How do I ingest a PDF and query the resulting graph?"
```

`memory ask` answers questions about the platform, its CLI, API, and concepts,
and can run tasks against your project.

CLI only, against an existing server:

```bash
curl -fsSL https://raw.githubusercontent.com/emergent-company/emergent.memory/main/deploy/install-cli.sh | bash

export MEMORY_SERVER_URL=http://localhost:3002
export MEMORY_ACCOUNT_API_KEY=<your-api-key>
memory status
```

Upgrades: `memory upgrade`.

Self-hosting details are in [`deploy/self-hosted/`](deploy/self-hosted/README.md).

## Repository layout

This is a monorepo:

| Path | What it is |
|---|---|
| `apps/server` | Go server (Echo + Bun ORM + fx), Zitadel or standalone auth |
| `apps/web-ui` | Web UI (Go templ + HTMX gateway) |
| `apps/cli` | `memory` CLI |
| `apps/connector.linux`, `apps/connector.mac`, `apps/ios` | Platform connectors and Apple apps |
| `sdk/`, `swift/` | Go, Python, and Swift SDKs |
| `blueprints/` | Example agent and schema packs |
| `deploy/self-hosted/` | Self-hosted installers and Compose stack |
| `docs/site/` | Published documentation (mkdocs) |
| `e2e/` | End-to-end test suite |

## Development

Requirements: **Go 1.26+**, **Docker with Compose v2**, **Task**, **Node 20+**
(for the web UI). Build, test, and local-stack entry points are in
[`CONTRIBUTING.md`](CONTRIBUTING.md).

## Documentation

- Published documentation: **https://emergent-company.github.io/emergent.memory/**
- API reference: [`apps/server/docs/swagger/`](apps/server/docs/swagger/)
- Database schema: [`docs/schema.dbml`](docs/schema.dbml)

## License

This project is licensed under the [MIT License](LICENSE) (© 2024-2026 Emergent
Company). Third-party components are attributed in [NOTICE](NOTICE).
