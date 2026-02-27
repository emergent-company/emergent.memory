## Why

The Go backend currently has no local observability — traces are either sent to SigNoz (external SaaS) or Langfuse (AI-specific, now removed). Developers need a lightweight, self-hosted trace store they fully control: one that starts with Docker Compose, imposes no cloud dependency, and can be queried from the CLI or a future admin UI without requiring a third-party dashboard.

## What Changes

- **Add Grafana Tempo** as an opt-in Docker Compose service (profile: `observability`) with local filesystem storage and configurable retention
- **Instrument the Go/Echo server** with OpenTelemetry SDK: trace middleware for all HTTP routes, span propagation through service/store layers, OTLP exporter that activates only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set
- **Add `emergent traces` subcommand** to the CLI for querying Tempo directly (list recent traces, fetch by ID, filter by service/route/duration)
- **Add retention policy config**: `OTEL_RETENTION_HOURS` controls how long Tempo keeps trace data (default: 720h / 30 days)
- **Remove SigNoz references** from config, docs, and `.vscode/mcp.json` (already partially done)

## Capabilities

### New Capabilities

- `otel-tracing`: Local OpenTelemetry trace collection and querying via Grafana Tempo — covers the Tempo deployment, OTLP instrumentation on the Go server, trace retention policy, and CLI query interface

### Modified Capabilities

- `deployment`: Tempo added as optional Docker Compose service under `observability` profile

## Impact

- `apps/server-go/` — OTel SDK packages added to `go.mod`, trace middleware wired into Echo, OTLP exporter configured via env vars
- `docker/tempo/` — new directory with `tempo.yaml` config
- `docker-compose.yml` (or equivalent) — new `tempo` service under `observability` profile
- `tools/emergent-cli/` — new `traces` subcommand
- `AGENTS.md`, `.opencode/instructions.md` — updated with tracing commands
