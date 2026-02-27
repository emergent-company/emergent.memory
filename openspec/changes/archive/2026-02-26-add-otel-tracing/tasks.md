## 1. Tempo Docker infrastructure

- [x] 1.1 Create `docker/tempo/tempo.yaml` — Tempo config with OTLP receivers (gRPC + HTTP), local filesystem storage, WAL, and `block_retention: ${OTEL_RETENTION_HOURS:-720}h`
- [x] 1.2 Add `tempo` service to `docker-compose.dev.yml` under `profiles: [observability]` with volume `tempo_data`, ports 4317/4318/3200, and config mount
- [x] 1.3 Add `tempo_data` named volume to `docker-compose.dev.yml` volumes section

## 2. Go server OTel instrumentation

- [x] 2.1 Create `apps/server-go/domain/tracing/config.go` — `OtelConfig` struct with `ExporterEndpoint`, `ServiceName`, `SamplingRate` fields; embed in root `Config`
- [x] 2.2 Create `apps/server-go/domain/tracing/module.go` — fx module that initialises TracerProvider (OTLP HTTP exporter if endpoint set, no-op otherwise), registers global provider, returns Echo middleware
- [x] 2.3 Add `otelecho` and `otlptracehttp` to `go.mod` as direct dependencies (`go get go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho` and promote `otlptracehttp`)
- [x] 2.4 Register `tracing.Module` in `apps/server-go/cmd/server/main.go` fx app
- [x] 2.5 Add `OtelConfig` to `apps/server-go/internal/config/config.go`
- [x] 2.6 Add `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME`, `OTEL_SAMPLING_RATE`, `OTEL_RETENTION_HOURS` to `.env.example` (commented out, with description)

## 3. CLI `traces` subcommand

- [x] 3.1 Create `tools/emergent-cli/internal/cmd/traces.go` — Cobra command `traces` with persistent flag `--tempo-url` (default `http://localhost:3200`), reads `EMERGENT_TEMPO_URL` env var
- [x] 3.2 Implement `emergent traces list [--since 1h] [--limit 20]` — calls `GET /api/search`, renders table with traceID (short), root span name, duration, status code, timestamp
- [x] 3.3 Implement `emergent traces search [--service s] [--route r] [--min-duration d] [--since t] [--limit n]` — builds TraceQL query from flags, calls `/api/search`, same table output
- [x] 3.4 Implement `emergent traces get <traceID>` — calls `GET /api/traces/<id>`, renders span tree (root span → children, indented, each showing name, duration, status, key HTTP attrs)
- [x] 3.5 Register `TracesCmd` in `tools/emergent-cli/internal/cmd/root.go`

## 4. Documentation updates

- [x] 4.1 Update `AGENTS.md` — add tracing section: how to enable (`OTEL_EXPORTER_OTLP_ENDPOINT`), how to start Tempo, `emergent traces list` usage
- [x] 4.2 Update `.opencode/instructions.md` — add observability section with Tempo start command and trace query examples
- [x] 4.3 Add `task traces:list` and `task traces:get` convenience tasks to root `Taskfile.yml` (delegating to `emergent traces`)

## 5. Validate

- [x] 5.1 Run `task build` — server compiles with tracing module
- [x] 5.2 Start Tempo with `docker compose --profile observability up tempo -d`, verify `/api/search` responds on port 3200
- [x] 5.3 Start server with `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318`, make a request, verify `emergent traces list` shows it
- [x] 5.4 Start server without `OTEL_EXPORTER_OTLP_ENDPOINT`, verify server starts cleanly with no tracing errors in logs
- [x] 5.5 Build CLI (`go build ./...` in `tools/emergent-cli`) — verify `emergent traces --help` works
