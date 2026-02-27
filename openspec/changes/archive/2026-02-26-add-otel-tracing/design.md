## Context

The Go server already pulls in OTel packages as indirect dependencies (via ADK-Go). The OTel SDK (`go.opentelemetry.io/otel/sdk v1.40.0`), core API, and HTTP OTLP exporter are all present in `go.mod` as indirects — they just need to be promoted and wired up. No echo-specific OTel middleware is present yet; `otelecho` from `go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho` will be added.

Grafana Tempo runs as a single Go binary with no external database. It accepts OTLP/gRPC on port 4317 and OTLP/HTTP on 4318, stores trace blocks on the local filesystem, and exposes a stable HTTP query API on port 3200. In monolithic mode it is a single Docker container with a mounted volume and YAML config.

The `emergent-cli` uses Cobra + a `client` package for API calls. A new `traces` top-level command fits the existing pattern (analogous to `tokens`, `agents`, etc.).

## Goals / Non-Goals

**Goals:**
- Add Tempo as an opt-in Docker Compose service (profile: `observability`)
- Instrument all Echo HTTP routes with OTel trace spans (method, route, status)
- Propagate trace context through the `context.Context` chain into service/store layers
- OTLP exporter activates only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set — no-op otherwise
- Configurable retention via `OTEL_RETENTION_HOURS` env var (Tempo `block_retention`)
- `emergent traces` CLI subcommand: `list`, `get <traceID>`, `search` with filters

**Non-Goals:**
- Metrics or logs collection (traces only for now)
- Distributed tracing across multiple services (single-service span propagation only)
- Custom span instrumentation inside every store/service call (too invasive; HTTP boundary spans only in the first pass)
- Replacing the existing `domain/monitoring` extraction-job monitoring (that stays as-is)

## Decisions

**D1: Conditional OTLP exporter — no-op when endpoint not set**
Config struct gets `OtelConfig` with `ExporterEndpoint string` (env: `OTEL_EXPORTER_OTLP_ENDPOINT`, default: `""`). The `domain/tracing` fx module checks this at startup: if empty → install a no-op TracerProvider. If set → install OTLP HTTP exporter targeting that endpoint. This makes the feature fully optional with zero overhead when disabled.

**D2: `domain/tracing` fx module owns provider lifecycle**
A new `domain/tracing` package provides `fx.Module("tracing", ...)` that:
- Creates and registers the global `TracerProvider`
- Adds the Echo `otelecho` middleware to the Echo instance
- Registers an `fx.Hook` to call `TracerProvider.Shutdown()` on app stop
This keeps tracing wiring in one place, consistent with the existing fx module pattern.

**D3: OTLP HTTP exporter over gRPC**
The HTTP exporter (`otlptracehttp`) is already an indirect dependency. It's simpler (no gRPC dependency chain), works over standard HTTP/1.1 or HTTP/2, and Tempo accepts it on port 4318. Avoids adding the gRPC exporter package.

**D4: Tempo config baked into `docker/tempo/tempo.yaml`; retention exposed as env var**
The Tempo config file is static YAML in `docker/tempo/tempo.yaml`. The only user-facing knob is `OTEL_RETENTION_HOURS` (default: `720` = 30 days), which is substituted via `envsubst` in the Docker Compose `command`. This avoids templating complexity while giving users the one config they care about.

**D5: `emergent traces` CLI queries Tempo HTTP API directly**
Tempo's `/api/search` and `/api/traces/<id>` endpoints are stable and well-documented. The CLI adds a `--tempo-url` flag (default: `http://localhost:3200`) and `EMERGENT_TEMPO_URL` env var. No auth needed for local Tempo. Commands:
- `emergent traces list` — recent traces (last 1h, limit 20)
- `emergent traces search --service <s> --route <r> --min-duration <d> --since <t>`
- `emergent traces get <traceID>` — fetch full trace, display as span tree

**D6: Service name set to `emergent-server`**
OTel resource attribute `service.name=emergent-server` hardcoded. The `service.version` comes from the existing version constant. This is visible in Tempo search results and in any Grafana dashboard added later.

## Tempo Docker Compose Design

```yaml
# In docker-compose.dev.yml — added under services:
tempo:
  image: grafana/tempo:latest
  container_name: emergent-tempo
  command: ["-config.file=/etc/tempo.yaml"]
  volumes:
    - ./docker/tempo/tempo.yaml:/etc/tempo.yaml:ro
    - tempo_data:/var/tempo
  ports:
    - "${TEMPO_OTLP_GRPC_PORT:-4317}:4317"   # OTLP gRPC (conflicts if server uses 4317)
    - "${TEMPO_OTLP_HTTP_PORT:-4318}:4318"   # OTLP HTTP
    - "${TEMPO_HTTP_PORT:-3200}:3200"         # Query API + UI
  restart: unless-stopped
  profiles: ["observability"]
```

Note: ports 4317/4318 are the standard OTLP ports. If the host already uses them, override via env vars.

## `domain/tracing` Package Design

```
apps/server-go/domain/tracing/
  module.go      # fx.Module, provider init, Echo middleware registration
  config.go      # OtelConfig struct (embedded in root Config)
  noop.go        # no-op provider for when endpoint is not set
```

`OtelConfig` fields:
- `ExporterEndpoint string` — `OTEL_EXPORTER_OTLP_ENDPOINT` (default: `""`)
- `ServiceName string` — `OTEL_SERVICE_NAME` (default: `"emergent-server"`)
- `SamplingRate float64` — `OTEL_SAMPLING_RATE` (default: `1.0` = 100%)

## `emergent traces` CLI Design

New file: `tools/emergent-cli/internal/cmd/traces.go`

```
emergent traces list [--since 1h] [--limit 20] [--tempo-url ...]
emergent traces search [--service s] [--route /api/...] [--min-duration 200ms] [--since 1h]
emergent traces get <traceID>
```

Output: table for `list`/`search` (traceID, service, root span, duration, status, timestamp), JSON or tree for `get`.

## Risks / Trade-offs

- **Port conflicts**: 4317/4318 are standard OTLP ports. If another OTel agent is running, there will be conflicts. Mitigated by `${TEMPO_OTLP_GRPC_PORT:-4317}` overrides.
- **Disk usage**: Tempo stores raw trace blocks. A busy server with 100% sampling could generate significant data. Mitigated by `OTEL_SAMPLING_RATE` env var and 30-day default retention.
- **No WAL durability in minimal config**: Tempo's WAL flushes to block storage; if the container crashes mid-flush, recent traces can be lost. Acceptable for a dev/local observability tool.
