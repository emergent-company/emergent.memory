## Why

Current OpenTelemetry traces cover only the outermost entry and exit points of operations — the interior of every request is a black box. When a request is slow or fails, traces cannot identify *which* sub-operation (DB query, embedding call, tool execution, workspace provisioning, parallel search) caused the problem, making traces useful only for confirming that something happened rather than diagnosing why.

## What Changes

- Add child spans for all embedding generation calls (`EmbedQuery`, `EmbedQueryWithUsage`) across chat, search, and extraction paths
- Add child spans for parallel search sub-operations (graph search, text search, relationship search) with proper goroutine context propagation
- Add child spans for workspace provisioning (`ProvisionForSession`, `LinkToRun`, `TeardownWorkspace`) in the agent executor
- Add per-tool child spans in agent execution (tool name, input size, output size, success/error per invocation)
- Add child spans for LLM model creation (`modelFactory.CreateModel`) in agent executor and extraction pipeline
- Fix async goroutines that detach from parent span context: RAG search goroutine in chat handler, background message persistence, access timestamp updates in search
- Add child spans for hot-path repository calls: `AddMessage`, `HybridSearch`, `graphService.Create`, `graphService.CreateRelationship`
- Add missing attributes to existing spans: `emergent.agent.max_steps`, `emergent.document.size_bytes`, `emergent.search.result_types`, `emergent.extraction.retry_count`, `emergent.error.type`
- Add `agent.tool_call` events to the `agent.run` span for doom-loop detection visibility
- Add stream lifecycle events to `chat.llm_generate`: time-to-first-token, stream complete/abort

## Capabilities

### New Capabilities
- `trace-instrumentation`: Requirements for deep, systematic OpenTelemetry trace coverage across chat, agent, extraction, and search domains — sub-spans, goroutine context propagation, required span attributes, and streaming event instrumentation

### Modified Capabilities

(none — existing specs cover setup and high-level structure; this change adds depth below those layers)

## Impact

- `apps/server-go/domain/chat/handler.go` — async goroutine fixes, RAG search span, message persistence span
- `apps/server-go/domain/agents/executor.go` — workspace spans, tool call spans, model creation span, doom-loop events
- `apps/server-go/domain/extraction/agents/pipeline.go` — model creation span, runner execution span, session operation spans
- `apps/server-go/domain/extraction/agents/quality_checker.go` — sub-spans for state reads and orphan calculation
- `apps/server-go/domain/extraction/*_worker.go` — storage I/O spans, chunking spans, DB write spans
- `apps/server-go/domain/search/service.go` — parallel goroutine context fix, sub-spans for graph/text/relationship search, embedding span, fusion span
- `apps/server-go/pkg/tracing/tracer.go` — no changes needed (helper is adequate)
- No API changes, no schema changes, no new dependencies
