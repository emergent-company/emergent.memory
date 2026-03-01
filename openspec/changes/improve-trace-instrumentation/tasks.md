## 1. Tracing Helper (pkg/tracing)

- [ ] 1.1 Add `StartLinked(parentCtx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span)` helper in `pkg/tracing/tracer.go` that creates a new root span linked to the parent span via `trace.WithLinks` — to be used for background goroutines that outlive the HTTP request
- [ ] 1.2 Add `RecordErrorWithType(span trace.Span, err error)` helper that calls `span.RecordError(err)`, `span.SetStatus(codes.Error, err.Error())`, and sets `emergent.error.type` attribute using `reflect.TypeOf(err).String()`

## 2. Search Service (domain/search/service.go)

- [ ] 2.1 Refactor parallel search goroutines (graph, text, relationship) to use `errgroup` with the parent span context instead of raw goroutines + channels
- [ ] 2.2 Add `search.graph_search` child span inside the graph search goroutine with attribute `emergent.search.sub_result_count`
- [ ] 2.3 Add `search.text_search` child span inside the text search goroutine with attribute `emergent.search.sub_result_count`
- [ ] 2.4 Add `search.relationship_search` child span inside the relationship search goroutine with attribute `emergent.search.sub_result_count`
- [ ] 2.5 Wrap `s.embeddings.EmbedQuery()` call in a `embedding.generate` child span with attribute `emergent.embedding.input_length`
- [ ] 2.6 Wrap `s.fuseResults()` call in a `search.fuse_results` child span with attributes `emergent.search.fusion_strategy` and `emergent.search.input_count`
- [ ] 2.7 Wrap `s.graphService.HybridSearch()` call in a `search.hybrid_search` child span
- [ ] 2.8 Fix background access timestamp goroutine: capture span link from `search.execute` before launching goroutine, use `StartLinked` inside the goroutine so errors are recorded rather than silently discarded
- [ ] 2.9 Add `emergent.search.result_types` attribute to `search.execute` span after sub-searches complete, recording which sources returned results (e.g., "graph,text")

## 3. Chat Handler (domain/chat/handler.go)

- [ ] 3.1 Wrap the RAG search goroutine: propagate span context into the goroutine and create a `chat.rag_search` child span inside it
- [ ] 3.2 Fix background message persistence goroutine: use `StartLinked` to create a linked span and record errors on the span instead of discarding them with `_, _`
- [ ] 3.3 Add `chat.persist_message` child span wrapping the `svc.AddMessage` call in the streaming path
- [ ] 3.4 Add streaming lifecycle events to `chat.llm_generate` span: emit `llm.stream_start` event with `emergent.llm.time_to_first_token_ms` on first token received
- [ ] 3.5 Emit `llm.stream_end` event on `chat.llm_generate` with `emergent.llm.finish_reason` set to "complete", "error", or "client_disconnect" based on how the stream terminates

## 4. Agent Executor (domain/agents/executor.go)

- [ ] 4.1 Add `emergent.agent.max_steps` attribute to the `agent.run` span at span creation
- [ ] 4.2 Wrap `ae.provisioner.ProvisionForSession()` in an `agent.workspace_provision` child span with attribute `emergent.agent.workspace_id` set after provisioning succeeds
- [ ] 4.3 Wrap `ae.provisioner.TeardownWorkspace()` in an `agent.workspace_teardown` child span; record error if teardown fails
- [ ] 4.4 Wrap `modelFactory.CreateModel()` call in an `agent.model_create` child span with attribute `emergent.llm.model`
- [ ] 4.5 In the `afterToolCb` callback: record a span event named `agent.tool_call` on the `agent.run` span with attributes `emergent.agent.tool_name`, `emergent.agent.tool_success`, and `emergent.agent.tool_input_size`
- [ ] 4.6 In the `afterToolCb` callback: if tool execution duration exceeds 200ms, additionally create a child span `agent.tool_call` under `agent.run` with the same attributes and the correct start/end times

## 5. Extraction Pipeline (domain/extraction/agents/pipeline.go)

- [ ] 5.1 Wrap `p.modelFactory.CreateModel()` in an `extraction.model_create` child span with attribute `emergent.llm.model`
- [ ] 5.2 Wrap the `runner.Run()` / `r.Run()` call in an `extraction.runner_execute` child span

## 6. Object Extraction Worker (domain/extraction/object_extraction_worker.go)

- [ ] 6.1 Wrap `w.graphService.Create()` calls in `extraction.persist_graph_objects` child spans under `extraction.object_extraction`
- [ ] 6.2 Wrap `w.graphService.CreateRelationship()` calls in `extraction.persist_relationships` child spans under `extraction.object_extraction`
- [ ] 6.3 Track quality-check retry iteration count and set `emergent.extraction.retry_count` attribute on `extraction.object_extraction` span at completion

## 7. Document Parsing Worker (domain/extraction/document_parsing_worker.go)

- [ ] 7.1 Add `emergent.document.size_bytes` attribute to the `extraction.document_parsing` span after the document is loaded (set to the byte length of the raw content)

## 8. Embedding Workers (domain/extraction/chunk_embedding_worker.go, relationship_embedding_worker.go, graph_embedding_worker.go)

- [ ] 8.1 Wrap `w.embeds.EmbedQueryWithUsage()` call in `chunk_embedding_worker.go` in an `embedding.generate` child span with attribute `emergent.embedding.input_length`
- [ ] 8.2 Wrap embedding call in `relationship_embedding_worker.go` in an `embedding.generate` child span with attribute `emergent.embedding.input_length`
- [ ] 8.3 Wrap embedding call in `graph_embedding_worker.go` in an `embedding.generate` child span with attribute `emergent.embedding.input_length`

## 9. Error Attribute Standardization

- [ ] 9.1 Replace all `span.RecordError(err); span.SetStatus(codes.Error, err.Error())` patterns across all modified files with the new `RecordErrorWithType(span, err)` helper from task 1.2
- [ ] 9.2 Verify no modified span ends in error status without the `emergent.error.type` attribute set

## 10. Validation

- [ ] 10.1 Build the server (`go build ./...`) with no errors after all changes
- [ ] 10.2 Run existing tests (`go test ./domain/search/... ./domain/chat/... ./domain/agents/... ./domain/extraction/...`) with no regressions
- [ ] 10.3 With a local OTEL endpoint configured, trigger a chat request and confirm `chat.rag_search`, `chat.persist_message`, and `embedding.generate` appear as children of `chat.handle_message` in the trace backend
- [ ] 10.4 Trigger a search request and confirm `search.graph_search`, `search.text_search`, `search.relationship_search`, and `search.fuse_results` appear as parallel children of `search.execute`
- [ ] 10.5 Trigger an agent run and confirm `agent.workspace_provision`, `agent.model_create`, and `agent.tool_call` events appear on the `agent.run` span
- [ ] 10.6 Trigger an extraction job and confirm `extraction.model_create`, `embedding.generate`, `extraction.persist_graph_objects` appear in the trace
