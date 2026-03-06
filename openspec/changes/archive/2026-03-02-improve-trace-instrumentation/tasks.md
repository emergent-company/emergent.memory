## 1. Tracing Helper (pkg/tracing)

- [x] 1.1 Add `StartLinked(parentCtx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span)` helper in `pkg/tracing/tracer.go` that creates a new root span linked to the parent span via `trace.WithLinks` — to be used for background goroutines that outlive the HTTP request. Must extract `trace.SpanFromContext(parentCtx).SpanContext()` *before* launching the goroutine if used across boundaries.
- [x] 1.2 Add `RecordErrorWithType(span trace.Span, err error)` helper that calls `span.RecordError(err)`, `span.SetStatus(codes.Error, err.Error())` unconditionally, and sets `emergent.error.type` attribute using `reflect.TypeOf(err).String()`. Only use for actual operational faults, not expected user input validation.
- [x] 1.3 Add a unit test file `tracer_test.go` using `go.opentelemetry.io/otel/sdk/trace/tracetest` to assert `StartLinked` properly configures span links and `RecordErrorWithType` correctly populates attributes and sets error status.

## 2. Search Service (domain/search/service.go)

- [x] 2.1 Refactor parallel search goroutines (graph, text, relationship) to use `sync.WaitGroup` (or `errgroup.Group` without `WithContext`) and mutex/channels to collect partial results without context cancellation on first error.
- [x] 2.2 Add `search.graph_search` child span inside the graph search goroutine with attribute `emergent.search.sub_result_count`
- [x] 2.3 Add `search.text_search` child span inside the text search goroutine with attribute `emergent.search.sub_result_count`
- [x] 2.4 Add `search.relationship_search` child span inside the relationship search goroutine with attribute `emergent.search.sub_result_count`
- [x] 2.5 Wrap `s.embeddings.EmbedQuery()` call in a `embedding.generate` child span with attribute `emergent.embedding.input_length`
- [x] 2.6 Wrap `s.fuseResults()` call in a `search.fuse_results` child span with attributes `emergent.search.fusion_strategy` and `emergent.search.input_count`
- [x] 2.7 Wrap `s.graphService.HybridSearch()` call in a `search.hybrid_search` child span
- [x] 2.8 Fix background access timestamp goroutine: extract parent `SpanContext` *before* launching goroutine, use `StartLinked` inside the goroutine so errors are recorded.
- [x] 2.9 Add `emergent.search.result_types` attribute to `search.execute` span after sub-searches complete, recording which sources returned results (e.g., "graph,text")

## 3. Chat Handler (domain/chat/handler.go)

- [x] 3.1 Wrap the synchronous `h.searchSvc.Search` call in the RAG setup with a `chat.rag_search` child span.
- [x] 3.2 Fix background message persistence goroutine: extract parent `SpanContext` *before* goroutine launch, use `StartLinked` inside to create a linked span, and record errors using `RecordErrorWithType`.
- [x] 3.3 Add `chat.persist_message` child span wrapping the `svc.AddMessage` call in the streaming path.
- [x] 3.4 Add streaming lifecycle events to `chat.llm_generate` span: emit `llm.stream_start` event with `emergent.llm.time_to_first_token_ms` on first token received.
- [x] 3.5 Emit `llm.stream_end` event on `chat.llm_generate` with `emergent.llm.finish_reason` set to "complete", "error", or "client_disconnect" based on how the stream terminates.

## 4. Agent Executor (domain/agents/executor.go)

- [x] 4.1 Add `emergent.agent.max_steps` attribute to the `agent.run` span at span creation
- [x] 4.2 Wrap `ae.provisioner.ProvisionForSession()` in an `agent.workspace_provision` child span with attribute `emergent.agent.workspace_id` set after provisioning succeeds
- [x] 4.3 Wrap `ae.provisioner.TeardownWorkspace()` in an `agent.workspace_teardown` child span; record error if teardown fails
- [x] 4.4 Wrap `modelFactory.CreateModel()` call in an `agent.model_create` child span with attribute `emergent.llm.model`
- [x] 4.5 In the `afterToolCb` callback: record a span event named `agent.tool_call` on the `agent.run` span with attributes `emergent.agent.tool_name`, `emergent.agent.tool_success`, and `emergent.agent.tool_input_size`
- [x] 4.6 In the `afterToolCb` callback: if tool execution duration exceeds 200ms, additionally create a child span `agent.tool_call` under `agent.run` with the same attributes and the correct start/end times

## 5. Extraction Agents (domain/extraction/agents/...)

- [x] 5.1 Wrap `p.modelFactory.CreateModel()` in `pipeline.go` in an `extraction.model_create` child span with attribute `emergent.llm.model`
- [x] 5.2 Wrap the `runner.Run()` / `r.Run()` call in `pipeline.go` in an `extraction.runner_execute` child span
- [x] 5.3 Wrap state read and orphan calculation steps in `quality_checker.go` in `extraction.quality_check` child spans.

## 6. Extraction Workers (domain/extraction/*_worker.go & chunking)

- [x] 6.1 Add an `extraction.document_download` span in `document_parsing_worker.go` to capture storage I/O latency.
- [x] 6.2 Add an `extraction.chunking` span in `chunking/service.go` or where `chunk_embedding_worker.go` processes text chunks.
- [x] 6.3 Wrap `w.graphService.Create()` calls in `extraction.persist_graph_objects` child spans under `extraction.object_extraction` in `object_extraction_worker.go`
- [x] 6.4 Wrap `w.graphService.CreateRelationship()` calls in `extraction.persist_relationships` child spans under `extraction.object_extraction` in `object_extraction_worker.go`
- [x] 6.5 Track quality-check retry iteration count and set `emergent.extraction.retry_count` attribute on `extraction.object_extraction` span at completion
- [x] 6.6 Add `emergent.document.size_bytes` attribute to the `extraction.document_parsing` span after the document is loaded (set to the byte length of the raw content)
- [x] 6.7 Wrap `w.embeds.EmbedQueryWithUsage()` call in `chunk_embedding_worker.go` in an `embedding.generate` child span with attribute `emergent.embedding.input_length`
- [x] 6.8 Wrap embedding call in `graph_relationship_embedding_worker.go` in an `embedding.generate` child span with attribute `emergent.embedding.input_length`
- [x] 6.9 Wrap embedding call in `graph_embedding_worker.go` in an `embedding.generate` child span with attribute `emergent.embedding.input_length`

## 7. Error Attribute Standardization

- [x] 7.1 Replace all `span.RecordError(err); span.SetStatus(codes.Error, err.Error())` patterns across all modified files with the new `RecordErrorWithType(span, err)` helper from task 1.2
- [x] 7.2 Verify no modified span ends in error status without the `emergent.error.type` attribute set

## 8. Validation

- [x] 8.1 Build the server (`go build ./...`) with no errors after all changes
- [x] 8.2 Run all automated unit tests (`go test ./...`) with no regressions, including the new `tracetest` cases.
- [ ] 8.3 Run local tracing stack using `docker-compose -f docker-compose.dev.yml up -d jaeger` (or equivalent telemetry setup in docker).
- [ ] 8.4 Trigger a chat request and confirm `chat.rag_search`, `chat.persist_message`, and `embedding.generate` appear as children of `chat.handle_message` in the trace backend
- [ ] 8.5 Trigger a search request and confirm `search.graph_search`, `search.text_search`, `search.relationship_search`, and `search.fuse_results` appear as parallel children of `search.execute`
- [ ] 8.6 Trigger an agent run and confirm `agent.workspace_provision`, `agent.model_create`, and `agent.tool_call` events appear on the `agent.run` span
- [ ] 8.7 Trigger an extraction job and confirm `extraction.model_create`, `embedding.generate`, `extraction.persist_graph_objects`, and chunking spans appear in the trace
