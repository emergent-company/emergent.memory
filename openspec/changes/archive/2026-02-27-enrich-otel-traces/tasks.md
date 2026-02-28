## 1. Tracer Helper Package

- [x] 1.1 Create `apps/server-go/pkg/tracing/tracer.go` with `Start(ctx, name, attrs...)` helper wrapping `otel.Tracer("emergent").Start()`
- [x] 1.2 Ensure `pkg/tracing` is a no-op when no TracerProvider is set (it inherits the global no-op provider automatically — verify with a unit test or comment)

## 2. Extraction Worker Spans

- [x] 2.1 Instrument `DocumentParsingWorker.processJob()`: wrap job processing in `tracing.Start(ctx, "extraction.document_parsing", ...)` with `emergent.job.id`, `emergent.document.id`, `emergent.project.id`, `emergent.document.content_type`
- [x] 2.2 Instrument `ObjectExtractionWorker.processJob()`: wrap in `tracing.Start(ctx, "extraction.object_extraction", ...)` with `emergent.job.id`, `emergent.document.id`, `emergent.project.id`; add `emergent.extraction.entity_count` and `emergent.extraction.relationship_count` after pipeline completes
- [x] 2.3 Instrument `ChunkEmbeddingWorker` job processing: span `extraction.chunk_embedding` with `emergent.job.id`, `emergent.project.id`
- [x] 2.4 Instrument `GraphEmbeddingWorker` job processing: span `extraction.graph_embedding` with `emergent.job.id`, `emergent.project.id`
- [x] 2.5 Instrument `GraphRelationshipEmbeddingWorker` job processing: span `extraction.relationship_embedding` with `emergent.job.id`, `emergent.project.id`
- [x] 2.6 Add `span.RecordError(err)` and `span.SetStatus(codes.Error, ...)` on all error paths in the above workers

## 3. Extraction Pipeline Stage Spans

- [x] 3.1 In `ExtractionPipeline.Run()`, add child span `extraction.pipeline.extract_entities` around the entity extraction LLM call; carry `emergent.job.id` from ctx
- [x] 3.2 Add child span `extraction.pipeline.extract_relationships` around the relationship extraction LLM call
- [x] 3.3 Add child span `extraction.pipeline.quality_check` around `LogQualityCheck()`; set `emergent.extraction.orphan_rate` attribute; add event `extraction.quality_warning` if threshold exceeded

## 4. Agent Run Span

- [x] 4.1 In `AgentExecutor.Execute()` (or equivalent entry point), start span `agent.run` from the handler's `ctx` with attributes `emergent.agent.id`, `emergent.agent.run_id`, `emergent.project.id`
- [x] 4.2 Pass the span context into the async goroutine that runs the agent; use `defer span.End()` in the goroutine
- [x] 4.3 On run completion, add `emergent.agent.step_count` and `emergent.agent.run_status` to the span before ending
- [x] 4.4 On max-steps or cancellation, add the appropriate span event (`agent.max_steps_reached`) and set error status

## 5. Search and Chat Spans

- [x] 5.1 In `search.Service.ExecuteSearch()` (or the unified search handler), start child span `search.execute` with `emergent.search.query_length`, `emergent.search.strategy`, `emergent.project.id`; set `emergent.search.result_count` before ending
- [x] 5.2 In the chat message handler, start child span `chat.handle_message` with `emergent.chat.conversation_id`, `emergent.project.id`
- [x] 5.3 In the LLM generation step inside chat, start child span `chat.llm_generate` with `emergent.llm.model`

## 6. TUI Project-Scoped Trace Filtering

- [x] 6.1 Update `loadTraces(tempoURL string)` signature to `loadTraces(tempoURL, projectID string)` in `tempo.go`
- [x] 6.2 When `projectID` is non-empty, add TraceQL query param `q={span.emergent.project.id="<projectID>"}` to the Tempo `/api/search` request
- [x] 6.3 Pass `m.selectedProjectID` to `loadTraces` from the tab-switch handler and the back-to-TracesView path in `tui.go`
- [x] 6.4 When no project is selected, show a prompt in the Traces tab: "Select a project first to see its traces" (consistent with other project-scoped tabs)

## 7. TUI Agent Run Deep-Link

- [x] 7.1 In `renderTraceDetail()` in `tools/emergent-cli/internal/tui/tui.go`, detect span attribute `emergent.agent.run_id` and render it with a formatted link: `<server-url>/agents/runs/<run_id>` (derive server URL from `m.client.BaseURL()`)
- [x] 7.2 Display the deep-link prominently (e.g. highlighted line with label "Open in browser:") so it's easy to copy-paste
