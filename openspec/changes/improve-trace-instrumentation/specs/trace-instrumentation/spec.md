## ADDED Requirements

### Requirement: Embedding generation is traced
The system SHALL create a child span named `embedding.generate` for every call to `EmbedQuery` or `EmbedQueryWithUsage`, wrapping the span around the call and recording the result or error. The span SHALL be a child of the caller's active span (search, chat, extraction worker). The span SHALL include the attribute `emergent.embedding.input_length` (int, character count of the input text).

#### Scenario: Successful embedding in search path
- **WHEN** `search.execute` calls `EmbedQuery` to vectorize the user query
- **THEN** a child span `embedding.generate` appears under `search.execute` in the trace waterfall
- **AND** the span has attribute `emergent.embedding.input_length` set to the query character count
- **AND** the span status is Ok

#### Scenario: Embedding failure is recorded
- **WHEN** `EmbedQuery` returns an error during a search request
- **THEN** the `embedding.generate` span has status Error with the error message
- **AND** the parent `search.execute` span also records the error

#### Scenario: Multiple embedding calls produce multiple child spans
- **WHEN** the search service calls `EmbedQuery` for graph search and text search
- **THEN** two separate `embedding.generate` child spans appear under the parent, each with their own latency

---

### Requirement: Parallel search sub-operations are individually traced
The system SHALL create a child span for each parallel search sub-operation: `search.graph_search`, `search.text_search`, and `search.relationship_search`. Each goroutine SHALL receive the parent span context before being launched. Each sub-span SHALL record the number of results returned as `emergent.search.sub_result_count` (int) upon completion.

#### Scenario: All three sub-searches run and produce spans
- **WHEN** `search.execute` triggers a full hybrid search
- **THEN** three child spans — `search.graph_search`, `search.text_search`, `search.relationship_search` — appear under `search.execute` in the trace
- **AND** all three overlap in time (demonstrating parallel execution)

#### Scenario: A sub-search failure is isolated to its span
- **WHEN** graph search returns an error but text search succeeds
- **THEN** `search.graph_search` has status Error
- **AND** `search.text_search` has status Ok
- **AND** `search.execute` reflects the partial failure appropriately

#### Scenario: Sub-search result counts are recorded
- **WHEN** graph search returns 5 results and text search returns 12 results
- **THEN** `search.graph_search` has attribute `emergent.search.sub_result_count` = 5
- **AND** `search.text_search` has attribute `emergent.search.sub_result_count` = 12

---

### Requirement: Search result fusion is traced
The system SHALL create a child span named `search.fuse_results` covering the fusion strategy execution. The span SHALL include the attribute `emergent.search.fusion_strategy` (string) and `emergent.search.input_count` (int, total results before fusion).

#### Scenario: Fusion span captures strategy and input size
- **WHEN** search results from sub-searches are fused
- **THEN** a `search.fuse_results` span appears under `search.execute`
- **AND** it has `emergent.search.fusion_strategy` set to the active strategy name (e.g., "weighted", "rrf", "interleave")
- **AND** it has `emergent.search.input_count` set to the total number of candidate results fed into fusion

---

### Requirement: Workspace provisioning and teardown are traced
The system SHALL create child spans for workspace lifecycle operations within agent execution:
- `agent.workspace_provision` — wrapping `ProvisionForSession`
- `agent.workspace_teardown` — wrapping `TeardownWorkspace`

Each span SHALL be a child of the enclosing `agent.run` span. `agent.workspace_provision` SHALL include the attribute `emergent.agent.workspace_id` (string) if a workspace is allocated.

#### Scenario: Workspace provision appears as child of agent run
- **WHEN** an agent run requires workspace provisioning
- **THEN** `agent.workspace_provision` appears as a child of `agent.run` in the trace
- **AND** the span duration reflects actual provisioning latency

#### Scenario: Slow provisioning is visible in the waterfall
- **WHEN** workspace provisioning takes more than 2 seconds
- **THEN** the `agent.workspace_provision` span duration is accurately recorded
- **AND** the `agent.run` span's total duration includes this provisioning time

#### Scenario: Teardown failure is recorded
- **WHEN** `TeardownWorkspace` returns an error
- **THEN** the `agent.workspace_teardown` span has status Error with the error message

---

### Requirement: LLM model creation is traced
The system SHALL create a child span named `agent.model_create` or `extraction.model_create` (matching the domain) wrapping each call to `modelFactory.CreateModel`. The span SHALL include the attribute `emergent.llm.model` (string, the model identifier).

#### Scenario: Model creation latency is visible in agent runs
- **WHEN** an agent run initializes its LLM model
- **THEN** an `agent.model_create` child span appears under `agent.run`
- **AND** the span has attribute `emergent.llm.model` set to the configured model name

#### Scenario: Model creation failure propagates to parent span
- **WHEN** `modelFactory.CreateModel` returns an error
- **THEN** the `agent.model_create` span has status Error
- **AND** the parent span (`agent.run` or `extraction.object_extraction`) also reflects the failure

---

### Requirement: Agent tool invocations are recorded as span events
The system SHALL record each tool invocation during an agent run as a span event named `agent.tool_call` on the `agent.run` span. The event SHALL include attributes:
- `emergent.agent.tool_name` (string)
- `emergent.agent.tool_success` (bool)
- `emergent.agent.tool_input_size` (int, byte length of serialized input)

For tool calls with execution duration exceeding 200ms, the system SHALL additionally create a child span named `agent.tool_call` with the same attributes.

#### Scenario: Tool invocations appear as events on agent.run
- **WHEN** an agent calls three tools during a run
- **THEN** three `agent.tool_call` events appear on the `agent.run` span
- **AND** each event has `emergent.agent.tool_name`, `emergent.agent.tool_success`, and `emergent.agent.tool_input_size`

#### Scenario: Long tool calls also produce child spans
- **WHEN** a tool call takes more than 200ms to complete
- **THEN** a child span `agent.tool_call` appears under `agent.run` in the waterfall
- **AND** the span duration reflects the actual tool execution latency

#### Scenario: Failed tool calls are flagged in events
- **WHEN** a tool invocation returns an error
- **THEN** the corresponding `agent.tool_call` event has `emergent.agent.tool_success` = false

---

### Requirement: Async goroutines propagate parent span context
The system SHALL ensure that goroutines launched for background work (message persistence, access timestamp updates, RAG search) carry the parent span context. Background goroutines that outlive the HTTP request lifecycle SHALL use linked spans (not child spans) to preserve trace continuity without depending on a cancelled context.

#### Scenario: Background message persistence is linked to the originating trace
- **WHEN** a chat message is persisted asynchronously after the streaming response ends
- **THEN** a span for the persistence operation exists and is linked to the originating `chat.handle_message` span
- **AND** the span does not fail silently — errors are recorded on the span

#### Scenario: RAG search goroutine is part of the trace
- **WHEN** the chat handler launches a background RAG search
- **THEN** a `chat.rag_search` span is created within the goroutine using the propagated span context
- **AND** the span is a child of `chat.handle_message`

#### Scenario: Access timestamp updates in search do not lose trace context
- **WHEN** `search.execute` triggers background access timestamp updates
- **THEN** the background operation is linked to the originating `search.execute` span

---

### Requirement: Hot-path repository calls are traced
The system SHALL create child spans for the following repository calls on the critical path of user-facing operations:
- `chat.persist_message` — wrapping `svc.AddMessage` in the chat streaming path
- `search.hybrid_search` — wrapping `graphService.HybridSearch`
- `extraction.persist_graph_objects` — wrapping `graphService.Create` calls in the object extraction worker
- `extraction.persist_relationships` — wrapping `graphService.CreateRelationship` calls in the object extraction worker

Each span SHALL set status Error and record the error if the underlying call fails.

#### Scenario: Message persistence latency is visible
- **WHEN** the chat handler appends a message to the conversation
- **THEN** a `chat.persist_message` child span appears under the request trace
- **AND** its duration reflects the actual database write latency

#### Scenario: Graph persistence spans appear under extraction job
- **WHEN** object extraction writes entities to the graph
- **THEN** `extraction.persist_graph_objects` child spans appear under `extraction.object_extraction`

---

### Requirement: Required attributes are present on existing spans
The system SHALL add the following attributes to existing spans:

| Span | Attribute | Type | Description |
|------|-----------|------|-------------|
| `agent.run` | `emergent.agent.max_steps` | int | Configured maximum step budget |
| `extraction.document_parsing` | `emergent.document.size_bytes` | int | Raw byte size of the document |
| `search.execute` | `emergent.search.result_types` | string | Comma-separated list of sub-searches that returned results (e.g., "graph,text") |
| `extraction.object_extraction` | `emergent.extraction.retry_count` | int | Number of quality-check retry iterations |
| Any span ending in Error status | `emergent.error.type` | string | Go type name of the error (e.g., `*pgconn.PgError`) |

#### Scenario: agent.run span includes max_steps
- **WHEN** an agent run span is created
- **THEN** it has attribute `emergent.agent.max_steps` set to the configured step limit

#### Scenario: error.type is set on all error spans
- **WHEN** any span ends with status Error
- **THEN** the span has attribute `emergent.error.type` set to the runtime type of the error

#### Scenario: extraction retry count reflects actual iterations
- **WHEN** the extraction quality check requires 2 retries before passing
- **THEN** `extraction.object_extraction` has `emergent.extraction.retry_count` = 2 at span end

---

### Requirement: Streaming LLM responses record lifecycle events
The system SHALL record the following span events on the `chat.llm_generate` span during streaming:
- `llm.stream_start` — emitted when the first token is received, with attribute `emergent.llm.time_to_first_token_ms` (int, milliseconds from span start)
- `llm.stream_end` — emitted when the stream completes, with attribute `emergent.llm.finish_reason` (string: "complete", "abort", "error", "client_disconnect")

#### Scenario: Time-to-first-token is recorded
- **WHEN** the LLM streaming response delivers the first token
- **THEN** a `llm.stream_start` event is recorded on `chat.llm_generate`
- **AND** the event has `emergent.llm.time_to_first_token_ms` set to the elapsed milliseconds since span start

#### Scenario: Stream completion reason is recorded
- **WHEN** the LLM stream finishes normally
- **THEN** a `llm.stream_end` event is recorded with `emergent.llm.finish_reason` = "complete"

#### Scenario: Client disconnect is distinguishable from stream error
- **WHEN** the client disconnects mid-stream
- **THEN** `llm.stream_end` is recorded with `emergent.llm.finish_reason` = "client_disconnect"
- **AND** the span status remains Ok (disconnect is not a server error)
