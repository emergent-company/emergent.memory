## Context

OTel tracing is already wired: `domain/tracing/module.go` installs an OTLP (or no-op) `TracerProvider`, and the Echo middleware emits an HTTP span per request. The bun DB layer has no span instrumentation today — only a debug slog hook (`queryLoggingHook`) gated behind `cfg.Database.QueryDebug`. The goal is to add per-query spans so slow SQL is attributable inside a request trace, without changing the existing debug logging or adding any overhead when tracing is disabled.

## Decisions

### D1 — Use `github.com/uptrace/bun/extra/bunotel` rather than a hand-rolled hook

Adopt the official `bunotel.QueryHook` from the same `uptrace/bun` project we already depend on, instead of writing our own `bun.QueryHook`.

- **Why:** `bunotel` already computes the operation name, `db.system`/`db.operation`/`db.statement`, source-location attributes, and error status — all the required behavior — and it is maintained alongside bun, so it stays compatible with bun's query-event internals. A hand-rolled hook would duplicate that and risk drift.
- **Alternative considered:** a local `QueryHook` implementation — rejected: more code to maintain and test for no behavioral gain.

### D2 — Pin `bunotel@v1.2.16` to match `bun v1.2.16`

`bunotel` lives in the same repository as `bun` and shares its release tags, so pinning `v1.2.16` keeps the hook's expectation of bun's internal API aligned with the bun version we already build against (`v1.2.16`).

- **Why:** a mismatched version could compile against an incompatible `QueryEvent`/dialect surface. Same-tag is the safe default.

### D3 — Register the hook only when `cfg.Otel.Enabled()`

Gate registration on `cfg.Otel.Enabled()`, exactly mirroring how the Echo middleware is gated in `domain/tracing/module.go`.

- **Why:** when tracing is disabled the global provider is a no-op, so a registered hook would still allocate a span per query and burn CPU for nothing. Gating at registration makes the feature inert (zero overhead) when off, matching the existing tracing wiring.
- **Alternative considered:** always register and rely on the no-op tracer — rejected: avoids per-query span construction entirely.

### D4 — Leave `WithFormattedQueries` false

Do not enable `bunotel.WithFormattedQueries`; the default (`false`) records `db.statement` with placeholders only.

- **Why:** bound arguments never appear in the span, so no PII/user data leaks into traces, and statement cardinality stays low (one span series per query shape, not per argument combination). We do not need formatted queries for latency attribution.
- **Trade-off:** diagnosing a specific bad value is not possible from the span alone; the existing `DB_QUERY_DEBUG` hook remains the place to see fully-bound queries when needed.

### D5 — Keep `queryLoggingHook` unchanged when `cfg.Database.QueryDebug` is true

bun supports multiple query hooks (hooks run in registration order). We add the tracing hook and leave the existing debug-logging block in place.

- **Why:** debug query logging is a separate, opt-in surface; this change must not alter it. Registering both hooks is safe because bun invokes each `BeforeQuery`/`AfterQuery` in turn.
