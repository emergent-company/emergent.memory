## Why

OTel/OTLP tracing already exists: the server installs a TracerProvider, the Echo middleware emits an HTTP span per request, and several domains emit manual child spans. But database queries are invisible in the resulting traces. The only DB observability today is a slog debug hook gated behind `DB_QUERY_DEBUG`, which writes log lines that cannot be correlated with a trace waterfall. As a result, a slow SQL statement cannot be attributed to the request whose latency it consumes — the trace shows the request span's total duration with no child spans explaining where the time went.

## What Changes

- Register an OpenTelemetry `bun.QueryHook` on the bun DB so every query becomes a child span. The span is named by DB operation (`SELECT`, `INSERT`, `UPDATE`, `DELETE`, …) and carries `db.system`, `db.operation`, `db.statement`, and source-location attributes (`code.function` / `code.filepath` / `code.lineno`).
- Recording errors: a query that returns an error records the error on the span and sets span status `Error`.
- Disabled-when-tracing-off: the hook is registered only when `cfg.Otel.Enabled()` is true, so there is zero overhead when tracing is disabled (mirroring how the Echo middleware is gated in `domain/tracing/module.go`).
- Use the upstream `github.com/uptrace/bun/extra/bunotel` query hook rather than a hand-rolled one.

## Capabilities

### New Capabilities

- `observability-tracing`: emit an OTel span per SQL query so request latency can be attributed to SQL in Tempo.

### Modified Capabilities

<!-- None: no existing capability's requirements change. -->

## Impact

- `apps/server/internal/database/database.go`: register the `bunotel` query hook in `NewBunDB`, gated on `cfg.Otel.Enabled()`; existing `queryLoggingHook` (`DB_QUERY_DEBUG`) is untouched and coexists via bun's multiple-hook support.
- `apps/server/go.mod` / `apps/server/go.sum`: new dependency `github.com/uptrace/bun/extra/bunotel` (pinned to match `bun v1.2.16`).
- New test file `apps/server/internal/database/query_tracing_test.go` (in-memory span recorder + `sqlmock`; no real Postgres).
- No API, schema, or config-surface change. `db.system` is emitted automatically from the pgdialect; `db.name` is emitted from the configured database name.
