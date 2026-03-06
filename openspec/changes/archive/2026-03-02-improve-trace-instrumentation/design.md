## Context

The server uses `pkg/tracing/tracer.go` as a thin wrapper around `go.opentelemetry.io/otel/trace`. All spans are created via `tracing.Start(ctx, spanName, attrs...)` which returns `(context.Context, trace.Span)`. We will add a few helpers here to support linked spans and standardized error attributes, but the underlying OTEL setup requires no changes.

Current gaps fall into four categories:
1. **Missing sub-spans** — high-latency operations (embedding, workspace provisioning, tool calls, DB writes, quality checks, chunking) run inside traced parents with no child spans
2. **Detached goroutines** — two async paths (message persistence, access timestamp updates) launch goroutines that lose the parent span context
3. **Missing span attributes** — `max_steps`, `document.size_bytes`, `error.type`, `retry_count` omitted from existing spans
4. **Missing span events** — streaming lifecycle (time-to-first-token, stream done/abort) and doom-loop tool repetition not recorded

No new dependencies, no API changes, no data model changes.

## Goals / Non-Goals

**Goals:**
- Every latency-significant sub-operation has its own child span so trace waterfalls are actionable
- Async goroutines propagate parent span context; no fire-and-forget with silent failure
- Span attributes are sufficient to filter, group, and correlate traces in a backend (Tempo/Jaeger)
- Streaming responses record at minimum time-to-first-token and stream termination reason
- The build includes testing instructions to ensure the local OTEL stack is used to validate changes

**Non-Goals:**
- Database query instrumentation at the driver/ORM level (would require middleware; only hot-path service-layer calls are wrapped)
- Tracing every repository call everywhere — only calls on the critical path of user-facing operations
- Changing span export infrastructure, sampling config, or OTEL setup
- Adding metrics or logs — traces only

## Decisions

### D1: Wrap at call site, not inside the callee

**Decision:** Add spans in domain code at the call site (e.g., in `executor.go` before calling `provisioner.ProvisionForSession()`) rather than inside the infrastructure implementations.

**Rationale:** Infrastructure types (`Provisioner`, `EmbeddingService`, `GraphService`) are interfaces used in multiple contexts. Wrapping at the call site keeps tracing co-located with domain intent, lets each call site set domain-specific attributes, and avoids coupling infrastructure packages to the tracing package.

**Alternative considered:** Add tracing inside each infrastructure implementation. Rejected because it would scatter span naming decisions across packages and make attribute naming inconsistent.

---

### D2: Fix detached goroutines with explicit extraction protocol

**Decision:** For goroutines that must outlive the HTTP request (message persistence, access timestamp updates), the parent `SpanContext` MUST be extracted *before* launching the goroutine (`trace.SpanFromContext(ctx).SpanContext()`). Inside the goroutine, use `context.Background()` with a new helper `StartLinked` to create a root span that is linked to the parent.

**Rationale:** If we extract the span inside the goroutine, it represents a data race. If we pass the request `ctx` into the goroutine, it will be cancelled when the HTTP request ends, ruining the trace. Linked spans preserve the trace relationship without requiring the parent context to remain live.

**Alternative considered:** Wrap in a new root span with no link. Rejected because it breaks trace continuity.

---

### D3: Per-tool events on `agent.run`, not separate child spans per tool call

**Decision:** Record each tool invocation as a span event on the `agent.run` span (`agent.tool_call` event with tool name, input size, and success/error). Add a separate short child span only for tool calls that exceed a threshold (>200ms, to be set as a constant).

**Rationale:** Agents may call dozens of tools per run. Creating a child span per call at low volumes is fine, but at high step counts it generates excessive spans and makes waterfall views unreadable. Events are cheap, filterable, and sufficient for doom-loop analysis. Long tool calls warrant a full span.

---

### D4: Parallel search goroutines use wait-group without context cancellation

**Decision:** Refactor `search/service.go` parallel goroutine pattern to use `sync.WaitGroup` (or an `errgroup.Group` *without* `WithContext`) instead of raw goroutines + channels. Each goroutine creates a child span.

**Rationale:** The current implementation intentionally continues on partial failure (e.g. if graph search fails, text search still returns). If we used `errgroup.WithContext`, the first failure would cancel the context and kill the other searches. We must propagate the parent span context into the sub-searches but avoid cross-canceling them on partial failure.

**Alternative considered:** Keep raw goroutines, manually propagate span context. Works but is more error-prone and doesn't improve concurrency safety compared to a WaitGroup.

---

### D5: Span naming follows existing `<domain>.<operation>` convention

All new spans follow the pattern already established:
- `chat.rag_search` (synchronous), `chat.persist_message`
- `agent.workspace_provision`, `agent.workspace_teardown`, `agent.model_create`, `agent.tool_call` (for long calls)
- `extraction.model_create`, `extraction.runner_execute`, `extraction.quality_check`, `extraction.chunking`, `extraction.document_download`
- `search.embed_query`, `search.graph_search`, `search.text_search`, `search.relationship_search`, `search.fuse_results`
- `embedding.generate` (for calls in workers)

---

### D6: New required attributes use `emergent.*` namespace

All new attributes follow the established `emergent.<domain>.<field>` prefix:
- `emergent.agent.max_steps` (int) on `agent.run`
- `emergent.document.size_bytes` (int) on `extraction.document_parsing`
- `emergent.error.type` (string) on any span ending in error — set to the Go type name of the error
- `emergent.extraction.retry_count` (int) on `extraction.object_extraction`
- `emergent.search.result_types` (string, comma-separated: "graph,text,relationship") on `search.execute`
- `emergent.llm.time_to_first_token_ms` (int) as event attribute on `chat.llm_generate`

---

### D7: `RecordErrorWithType` Semantics

**Decision:** The new helper `RecordErrorWithType` MUST unconditionally call `span.RecordError(err)` and `span.SetStatus(codes.Error, ...)`. It should only be used for operational failures, not expected user validation errors (e.g., 400 Bad Request).

**Rationale:** We only want spans to turn "red" in the OTEL backend if there's a system fault or unexpected outcome. User input validation should be logged but not mark the system span as failed.

## Risks / Trade-offs

**[Risk] Goroutine span propagation is subtle to get right** → Mitigation: Use the linked-span pattern via `trace.ContextWithRemoteSpanContext` consistently. Detail the extraction protocol.

**[Risk] Parallel search goroutines short-circuiting** → Mitigation: Decision D4 ensures we do not use `errgroup.WithContext` for parallel operations where we want to survive partial failure.

**[Risk] Adding spans to hot paths (embedding, DB writes) has non-zero overhead** → Mitigation: OTEL span creation is ~200ns when tracing is enabled. Acceptable for production.

## Migration Plan / Testing Strategy

- No schema migrations, no config changes required
- **Local Testing:** Testing requires running the local `docker-compose.dev.yml` (which includes Tempo/Jaeger) to visually inspect traces during manual testing.
- **Automated Testing:** We will add unit tests using `go.opentelemetry.io/otel/sdk/trace/tracetest` for the new `tracer.go` helpers to ensure context extraction and error recording behave exactly as specified.
