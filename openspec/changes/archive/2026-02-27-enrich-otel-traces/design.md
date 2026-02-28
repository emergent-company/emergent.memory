## Context

The Go server already has a global `TracerProvider` registered via `otel.SetTracerProvider()` at startup. The otelecho middleware uses it automatically. Domain packages currently have no span instrumentation — they only use `slog` for logging and a separate file-based `ExtractionTraceLogger`.

Extraction jobs are background-polled (not HTTP-triggered), so they will never appear as child spans of an HTTP trace. Their spans must be root spans (new trace per job). Agent runs are HTTP-triggered (`POST /api/agents/:id/execute`) but do heavy async work after the HTTP response; the agent span must be started from the request context so it becomes a child of the HTTP span.

The key linkage the user wants: **trace → agent run → session browser**. Agent runs already have a stable UUID (`agent_run_id`). If we set this as a span attribute, the TUI trace detail view can render it as a browser-navigable link (e.g. `/agents/runs/:id`).

## Goals / Non-Goals

**Goals:**
- Spans for all extraction worker job types, covering key pipeline stages
- Span per search query execution and chat message handling
- Span per agent run, carrying `agent.run_id` for deep-linking
- TUI trace detail renders `agent.run_id` as a browser deep-link
- Thin `pkg/tracing` helper so domain packages don't scatter raw OTel API calls
- Never put variable-length payload content (text, prompts, embeddings) in span attributes

**Non-Goals:**
- Database query-level tracing (every SQL statement) — too noisy, deferred
- Distributed trace propagation to external services (Kreuzberg, Whisper, ADK) — deferred
- OpenTelemetry logs or metrics — traces only
- Modifying the file-based `ExtractionTraceLogger` (it serves a different purpose: full prompt/response capture for debugging)

## Decisions

### D1: Thin `pkg/tracing` helper, not per-domain tracers

Each domain package could call `otel.Tracer("domain-name")` directly. Instead, a shared `pkg/tracing` helper provides a `Start(ctx, spanName, attrs...)` wrapper. This keeps OTel API calls out of domain code, makes the naming convention consistent, and makes it easy to add attribute redaction later.

**Alternative considered:** Inject a `trace.Tracer` into each service via fx. Rejected — too much boilerplate for what is essentially a global.

### D2: Extraction job spans are root spans (new traces)

Extraction workers poll independently of HTTP requests. The `ctx` passed to `processJob()` has no parent span. This is correct: each extraction job is an independent unit of work. They will appear as separate traces in Tempo, searchable by `job.id` or `project.id`.

**Alternative considered:** Propagate a trace context through the job queue (store `traceparent` in the job record). Rejected — the jobs are created by HTTP requests that may have already completed; linking them would produce misleading trace timing.

### D3: Agent run span is a child of the HTTP span

`AgentExecutor.Execute()` is called from the HTTP handler context, so `ctx` carries the HTTP span. The agent span becomes a child, giving a complete view: HTTP request → agent execution → (tool calls as future children). The agent HTTP handler returns 202 Accepted quickly; the span should only end when the run completes, so it will outlive the HTTP response. This is fine — OTel spans are not HTTP-response-scoped.

**Alternative considered:** Make agent spans root spans. Rejected — losing the HTTP→agent linkage removes useful context (which API call triggered which run).

### D4: Attribute naming follows OpenTelemetry semantic conventions where applicable

Use `emergent.*` namespace for domain-specific attributes (`emergent.agent.run_id`, `emergent.job.id`, `emergent.project.id`, etc.). Use standard OTel semconv for `http.*`, `db.*`, `error.*`.

### D5: No payload content in attributes

Embedding vectors, document text, prompt text, LLM completions, extracted entity JSON — none of these go in span attributes. Attributes carry IDs, counts, and durations only. Large content is already captured by the file-based ExtractionTraceLogger and by `kb.agent_run_messages`.

### D6: TUI renders `emergent.agent.run_id` as a deep-link

The trace detail view in `emergent browse` already shows all span attributes. We add special-case rendering: when `emergent.agent.run_id` is present, render it as a URL pointing at the web UI's agent run page. The base URL is derived from the already-configured server URL.

## Risks / Trade-offs

- **Span outliving goroutine scope**: Agent run spans must be ended in the async goroutine, not the HTTP handler. If the goroutine panics without ending the span, it will be silently dropped by the batch exporter timeout (default 30s). Mitigation: use `defer span.End()` in the goroutine.
- **Extraction worker concurrency**: Multiple jobs process in parallel. Each gets its own span with its own `job.id`. No shared state between spans — safe.
- **Performance**: `tracer.Start()` on a no-op provider costs ~10ns. Attribute setting is similarly cheap. No performance concern.
- **Tempo cardinality**: `emergent.project.id` and `emergent.job.id` are high-cardinality. Tempo handles this fine (it's a trace backend, not a metrics backend). No concern.

## Migration Plan

- All new span code is additive. No existing behaviour changes.
- No database migrations required.
- Feature is gated by `OTEL_EXPORTER_OTLP_ENDPOINT` already — if not set, no-op tracer is used everywhere, zero overhead.
- Deploy as a regular release; no rollback complexity.
