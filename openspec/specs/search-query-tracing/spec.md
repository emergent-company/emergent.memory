## ADDED Requirements

### Requirement: Search execution span
The unified search service SHALL create an OTel child span for every call to `ExecuteSearch()`. The span SHALL record query metadata (length, strategy, result count) but never the query text or result content.

#### Scenario: Search span created as child of HTTP span
- **WHEN** `POST /search/unified` is called
- **THEN** the trace SHALL contain an HTTP parent span AND a child span named `search.execute`
- **AND** the child span SHALL carry: `emergent.search.query_length` (int, character count), `emergent.search.strategy` (string, e.g. `weighted`), `emergent.project.id`

#### Scenario: Search result count recorded on span
- **WHEN** the search completes
- **THEN** the span SHALL include `emergent.search.result_count` (integer)
- **AND** the span status SHALL be `ok` on success or `error` with `error.message` on failure

#### Scenario: Query text is not in span
- **WHEN** a search is executed with any query string
- **THEN** the span attributes SHALL NOT contain the query text, only `emergent.search.query_length`

### Requirement: Chat message handling span
The chat handler SHALL create a child span for each user message processed, covering the search retrieval and LLM response phases.

#### Scenario: Chat message span created
- **WHEN** `POST /api/chat/:id/messages` receives a user message
- **THEN** the trace SHALL contain a child span named `chat.handle_message`
- **AND** the span SHALL carry: `emergent.chat.conversation_id`, `emergent.project.id`

#### Scenario: Chat span records retrieval and generation sub-timings
- **WHEN** the chat message handler calls the search service and then the LLM
- **THEN** the `chat.handle_message` span SHALL contain child spans: `search.execute` (from the search service span) and `chat.llm_generate`
- **AND** `chat.llm_generate` SHALL carry `emergent.llm.model` (string) but NOT prompt text or response text

#### Scenario: No message content in chat spans
- **WHEN** a chat message is processed
- **THEN** span attributes SHALL NOT include user message text, retrieved chunk content, or LLM response text

### Requirement: TUI Traces tab filters by selected project
The Traces tab in `emergent browse` SHALL filter traces to the currently selected project by passing a TraceQL query to Tempo's search API. When no project is selected, the tab SHALL prompt the user to select one rather than showing all traces.

#### Scenario: Traces filtered to selected project
- **WHEN** a project is selected in the TUI and the user navigates to the Traces tab
- **THEN** `loadTraces` SHALL send `q={span.emergent.project.id="<projectID>"}` to Tempo
- **AND** only traces carrying that project's spans SHALL appear in the list

#### Scenario: No project selected shows prompt
- **WHEN** the user navigates to the Traces tab without a project selected
- **THEN** the tab SHALL display "Select a project first to see its traces"
- **AND** no Tempo API call SHALL be made
