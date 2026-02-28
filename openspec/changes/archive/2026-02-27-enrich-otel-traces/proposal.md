## Why

The current OTel setup only produces one span per HTTP request (via otelecho middleware). This gives us timing and status for endpoints but nothing about what happens inside them — extraction jobs, search queries, LLM calls, and agent runs are invisible. Richer traces will make it practical to diagnose slow extractions, failing agent runs, and expensive queries without grepping log files.

A second goal is to link traces directly to agent sessions: a trace for an agent run should carry the `agent.run_id` attribute so you can jump straight from a trace in the TUI to the agent browser to inspect the full conversation and tool calls.

## What Changes

- **Extraction workers** — add a parent span per job, child spans for each pipeline stage (document fetch, parsing, entity extraction, relationship extraction, quality check, embedding). Attributes: IDs and counts only — no chunk text, no prompt content.
- **Search and chat** — add a span per `ExecuteSearch()` call and per chat message handling, with query length and result count as attributes (no query text, no result content).
- **Agent executor** — add a span per `AgentRun` with `agent.run_id` and `agent.id` as attributes. This is the critical link: the TUI can read `agent.run_id` from the trace and surface a deep-link to the agent run browser.
- **Span helper package** — a thin `pkg/tracing` helper wrapping `otel.Tracer(...)` so domain packages don't import the OTel SDK directly.
- **TUI trace detail** — render `agent.run_id` as a clickable link in the trace detail view inside `emergent browse`.

## Capabilities

### New Capabilities

- `extraction-tracing`: OTel span instrumentation for all extraction pipeline workers and the ExtractionPipeline agent. Covers document parsing, object extraction, chunk embedding, graph embedding, and relationship embedding jobs.
- `agent-run-tracing`: OTel span per agent run execution with `agent.run_id` attribute. Enables navigating from a trace directly to the agent session browser.
- `search-query-tracing`: OTel spans for unified search and chat query execution paths.

### Modified Capabilities

- `otel-tracing`: Extends current requirements — adds instrumentation rules (no large payloads in attributes, span naming conventions, tracer helper package).

## Impact

- `apps/server-go/pkg/tracing/` — new helper package (tracer provider, span helpers)
- `apps/server-go/domain/extraction/` — instrumented workers and pipeline
- `apps/server-go/domain/agents/` — agent executor instrumentation
- `apps/server-go/domain/search/` — search service instrumentation
- `apps/server-go/domain/chat/` — chat handler instrumentation
- `tools/emergent-cli/internal/tui/tui.go` — render agent run link in trace detail view
- No new dependencies (OTel SDK already imported)
- No API changes
- No database schema changes
