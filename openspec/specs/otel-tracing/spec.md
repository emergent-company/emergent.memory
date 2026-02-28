### Requirement: Local trace collection via Grafana Tempo
The system SHALL provide an opt-in local OpenTelemetry trace backend using Grafana Tempo. Tempo SHALL be deployable as a single Docker container with no external database dependency. Trace data SHALL persist to a mounted volume and survive container restarts.

#### Scenario: Tempo starts with observability profile
- **WHEN** a developer runs `docker compose --profile observability up tempo`
- **THEN** Tempo SHALL start and accept OTLP traces on gRPC port 4317 and HTTP port 4318
- **AND** the Tempo query API SHALL be available on port 3200
- **AND** no additional services (databases, caches) SHALL be required

#### Scenario: Traces persist across restarts
- **WHEN** the Tempo container is restarted
- **THEN** previously collected traces SHALL remain queryable
- **AND** data SHALL be stored in the named Docker volume `tempo_data`

### Requirement: Go server emits OTLP traces conditionally
The Go/Echo server SHALL emit OpenTelemetry traces via OTLP HTTP when `OTEL_EXPORTER_OTLP_ENDPOINT` is set. When the env var is absent or empty, the server SHALL use a no-op tracer with zero overhead. No code path SHALL fail or log errors due to tracing being disabled.

#### Scenario: Tracing enabled via env var
- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318` is set and the server starts
- **THEN** every HTTP request SHALL produce an OTel span with attributes: `http.method`, `http.route`, `http.status_code`, `http.url`
- **AND** the span SHALL be exported to Tempo within 5 seconds of request completion

#### Scenario: Tracing disabled — no-op
- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` is not set
- **THEN** the server SHALL start normally with no tracing overhead
- **AND** no connection errors or warnings related to tracing SHALL appear in logs

#### Scenario: Service identity in spans
- **WHEN** a trace is collected
- **THEN** the root span resource SHALL include `service.name=emergent-server`
- **AND** the resource SHALL include `service.version` matching the server's build version

### Requirement: Configurable trace retention policy
Tempo SHALL automatically delete trace data older than a configurable retention period. The retention period SHALL be configurable without rebuilding the container.

#### Scenario: Default retention of 30 days
- **WHEN** Tempo runs with default configuration
- **THEN** trace blocks older than 720 hours (30 days) SHALL be automatically compacted and deleted

#### Scenario: Custom retention via env var
- **WHEN** `OTEL_RETENTION_HOURS=168` is set (7 days)
- **THEN** Tempo SHALL retain only traces from the last 168 hours
- **AND** the compactor SHALL remove older blocks on its next cycle

### Requirement: CLI trace query interface
The `emergent` CLI SHALL provide a `traces` subcommand for querying traces stored in Tempo. All commands SHALL work without authentication against a locally running Tempo instance.

#### Scenario: List recent traces
- **WHEN** a developer runs `emergent traces list`
- **THEN** the CLI SHALL display a table of recent traces (last 1 hour, up to 20 results)
- **AND** each row SHALL show: trace ID, root span name, duration, HTTP status, timestamp

#### Scenario: Search traces by criteria
- **WHEN** a developer runs `emergent traces search --route /api/kb/documents --min-duration 500ms`
- **THEN** the CLI SHALL return traces matching both filters
- **AND** results SHALL be sorted by start time descending

#### Scenario: Fetch full trace by ID
- **WHEN** a developer runs `emergent traces get <traceID>`
- **THEN** the CLI SHALL display the full span tree for that trace
- **AND** each span SHALL show: name, duration, status, key attributes

#### Scenario: Custom Tempo URL
- **WHEN** `EMERGENT_TEMPO_URL=http://myhost:3200` is set or `--tempo-url` flag is passed
- **THEN** all trace commands SHALL query that URL instead of the default `http://localhost:3200`

### Requirement: Shared tracer helper package
The server SHALL provide a shared `pkg/tracing` helper package that wraps `otel.Tracer()` and provides a consistent `Start(ctx, spanName, attrs ...attribute.KeyValue) (context.Context, trace.Span)` function. Domain packages SHALL use this helper rather than calling the OTel API directly.

#### Scenario: Domain package starts a span via helper
- **WHEN** any domain package needs to create a span
- **THEN** it SHALL call `tracing.Start(ctx, "operation.name", attrs...)` from `pkg/tracing`
- **AND** the returned span SHALL be a child of the span in `ctx` if one is present, or a root span otherwise

### Requirement: Span naming convention
All custom spans SHALL use dot-separated lowercase names following the pattern `<domain>.<operation>` (e.g. `extraction.document_parsing`, `agent.run`, `search.execute`). HTTP spans from otelecho follow their own convention and are excluded.

#### Scenario: Span names are consistent
- **WHEN** traces are queried in Tempo using a service name filter
- **THEN** all custom spans SHALL match the pattern `^[a-z]+\.[a-z_]+$`

### Requirement: Attribute payload safety rule
Span attributes SHALL be limited to: IDs (UUIDs as strings), counts (integers), durations (already captured by span timing), strategy/type enums (short strings), and error messages. Variable-length user content (document text, query text, prompt text, LLM completions, embeddings) SHALL NEVER appear in span attributes or span events.

#### Scenario: Attribute size stays bounded
- **WHEN** any span is exported
- **THEN** no individual attribute value SHALL exceed 256 characters
- **AND** attributes carrying entity names, document titles, or query text SHALL NOT be set
