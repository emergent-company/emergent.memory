## Purpose

Emit an OpenTelemetry span for every bun SQL query so request latency can be attributed to SQL in Tempo. The span becomes a child of the request span, named by DB operation, and carries database and source-location attributes plus error status. The instrumentation is inert when OTel tracing is disabled.

## ADDED Requirements

### Requirement: Emit a span per bun query named by DB operation

When OTel tracing is enabled, the server SHALL emit one OpenTelemetry span for every SQL query executed through bun, and the span's final name SHALL equal the DB operation (for example `SELECT`, `INSERT`, `UPDATE`, `DELETE`).

#### Scenario: A query emits an operation-named span

- **WHEN** OTel tracing is enabled and a `SELECT` query executes through bun
- **THEN** a single span is emitted whose name is `SELECT`

#### Scenario: No span when tracing is disabled

- **WHEN** OTel tracing is disabled
- **THEN** no query hook is registered and no query span is emitted

### Requirement: Populate database and source-location attributes

Each query span SHALL carry `db.system` with the value `postgresql`, `db.operation` equal to the operation, `db.statement` equal to the query text with placeholders (no bound arguments), and source-location attributes `code.function`, `code.filepath`, and `code.lineno`.

#### Scenario: Span carries db attributes

- **WHEN** a `SELECT` query executes under OTel tracing
- **THEN** the emitted span has `db.system=postgresql`, a `db.operation` attribute, and a non-empty `db.statement` attribute

#### Scenario: Span carries source-location attributes

- **WHEN** a query executes under OTel tracing
- **THEN** the emitted span has non-empty `code.function`, `code.filepath`, and `code.lineno` attributes pointing at the query's call site

### Requirement: Record query errors on the span

When a query returns an error, the span SHALL record the error and set span status to `Error`.

#### Scenario: Erroring query sets error status

- **WHEN** a query returns an error under OTel tracing
- **THEN** the span records the error and its status code is `Error`

### Requirement: Inert when tracing is disabled

The query hook SHALL be registered only when OTel tracing is enabled — that is, when the tracing feature (`FEATURE_TRACING`) is on AND an OTLP exporter endpoint (`OTEL_EXPORTER_OTLP_ENDPOINT`) is configured. When either is absent, no query hook SHALL be registered and there SHALL be zero per-query tracing overhead.

#### Scenario: OTLP endpoint not configured registers no hook

- **WHEN** `OTEL_EXPORTER_OTLP_ENDPOINT` is unset
- **THEN** the bun DB has no tracing query hook and executing a query produces zero spans

#### Scenario: Tracing feature disabled registers no hook

- **WHEN** `FEATURE_TRACING` is false, even with `OTEL_EXPORTER_OTLP_ENDPOINT` set
- **THEN** the bun DB has no tracing query hook and executing a query produces zero spans
