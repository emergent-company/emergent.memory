## Context

The server uses `pkg/tracing/tracer.go` as a thin wrapper around `go.opentelemetry.io/otel/trace`. All spans are created via `tracing.Start(ctx, spanName, attrs...)` which returns `(context.Context, trace.Span)`. The helper is adequate and requires no changes.

Current gaps fall into four categories:
1. **Missing sub-spans** — high-latency operations (embedding, workspace provisioning, tool calls, DB writes) run inside traced parents with no child spans
2. **Detached goroutines** — three async paths (RAG search, message persistence, access timestamp updates) launch goroutines that lose the parent span context
3. **Missing span attributes** — `max_steps`, `document.size_bytes`, `error.type`, `retry_count` omitted from existing spans
4. **Missing span events** — streaming lifecycle (time-to-first-token, stream done/abort) and doom-loop tool repetition not recorded

No new dependencies, no API changes, no data model changes.

## Goals / Non-Goals

**Goals:**
- Every latency-significant sub-operation has its own child span so trace waterfalls are actionable
- Async goroutines propagate parent span context; no fire-and-forget with silent failure
- Span attributes are sufficient to filter, group, and correlate traces in a backend (Tempo/Jaeger)
- Streaming responses record at minimum time-to-first-token and stream termination reason

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

### D2: Fix detached goroutines with context propagation, not background spans

**Decision:** For goroutines that must outlive the HTTP request (message persistence, access timestamp updates), pass a detached context created from the parent span: `trace.SpanFromContext(ctx).SpanContext()` used to link the new span as a linked span rather than a child.

**Rationale:** Using a child span with a cancelled context would cause the span to be dropped or malformed. Linked spans preserve the trace relationship without requiring the parent context to remain live.

**Alternative considered:** Wrap in a new root span with no link. Rejected because it breaks trace continuity — the async work would appear as an unrelated trace.

**Alternative considered:** Block the handler until persistence completes. Rejected because it adds latency to streaming responses.

---

### D3: Per-tool events on `agent.run`, not separate child spans per tool call

**Decision:** Record each tool invocation as a span event on the `agent.run` span (`agent.tool_call` event with tool name, input size, and success/error). Add a separate short child span only for tool calls that exceed a threshold (>200ms, to be set as a constant).

**Rationale:** Agents may call dozens of tools per run. Creating a child span per call at low volumes is fine, but at high step counts it generates excessive spans and makes waterfall views unreadable. Events are cheap, filterable, and sufficient for doom-loop analysis. Long tool calls warrant a full span.

**Alternative considered:** Always create child span per tool. Rejected — 50 tool calls × N concurrent agents = trace backend noise.

---

### D4: Parallel search goroutines use errgroup with context

**Decision:** Refactor `search/service.go` parallel goroutine pattern to use `golang.org/x/sync/errgroup` with a context derived from the parent span context. Each goroutine creates a child span before doing work.

**Rationale:** The current pattern uses raw goroutines + channels with no context propagation. `errgroup` provides structured concurrency with error collection and proper context cancellation. This is the established Go pattern for this case and is already used elsewhere in the codebase.

**Alternative considered:** Keep raw goroutines, manually propagate span context. Works but is more error-prone and doesn't improve error handling.

---

### D5: Span naming follows existing `<domain>.<operation>` convention

All new spans follow the pattern already established:
- `chat.rag_search`, `chat.persist_message`
- `agent.workspace_provision`, `agent.workspace_teardown`, `agent.model_create`, `agent.tool_call` (for long calls)
- `extraction.model_create`, `extraction.runner_execute`, `extraction.persist_results`
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

## Risks / Trade-offs

**[Risk] Goroutine span propagation is subtle to get right** → Mitigation: Use the linked-span pattern via `trace.ContextWithRemoteSpanContext` consistently. Add a helper in `pkg/tracing` if the pattern is repeated more than twice.

**[Risk] Per-tool events create large spans for long agent runs** → Mitigation: Decision D3 caps child span creation at >200ms calls; events remain lightweight regardless of count.

**[Risk] Wrapping parallel search goroutines may alter error behavior** → Mitigation: `errgroup` preserves first-error semantics. Existing fallback logic (continue on partial failure) is preserved inside each goroutine's span — only span context changes.

**[Risk] Adding spans to hot paths (embedding, DB writes) has non-zero overhead** → Mitigation: OTEL span creation is ~200ns when tracing is enabled. With the no-op provider (default when endpoint is unset), overhead is zero. Acceptable for production.

**[Risk] Span attribute `emergent.error.type` may leak internal type names** → Mitigation: Use `reflect.TypeOf(err).String()` which gives package-qualified names like `*pgconn.PgError` — acceptable for internal observability, not exposed to end users.

## Migration Plan

- No schema migrations, no config changes required
- Changes are additive: new spans and attributes appear when OTEL endpoint is configured
- Deploy as a normal server release; no rollback concerns (removing spans has no side effects)
- After deploy, validate in Tempo/Jaeger by checking that `search.graph_search`, `agent.workspace_provision`, and `embedding.generate` spans appear under their parents

## Open Questions

- Should `emergent.error.type` be standardized to short names (e.g., `pg_error`) or full Go type names? Short names require a mapping table but are more stable across refactors.
- Is 200ms the right threshold for promoting a tool event to a child span? Could be made configurable via env var if needed.
