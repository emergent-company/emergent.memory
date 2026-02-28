## Context

The chat service (`domain/chat/handler.go:StreamChat`) uses a direct Vertex AI HTTP client (`pkg/llm/vertex`) to stream responses. It does a single RAG pass: search -> inject context into system prompt -> stream LLM response. The LLM has no tools and cannot refine its search or traverse the graph.

Meanwhile, the agent executor (`domain/agents/executor.go`) uses ADK with full tool calling support via ToolPool, but operates in batch mode only -- it collects all events synchronously and returns a final result. There is no SSE streaming output. These are two completely separate LLM stacks with separate persistence models (`kb.chat_*` vs `kb.agent_run_*`).

The SSE event type `mcp_tool` is already defined in `pkg/sse/events.go` but unused -- it was designed for exactly this use case. The `MCPToolEvent` struct has `tool`, `status` ("started"/"completed"/"error"), `result`, and `error` fields.

The `Conversation` entity has an `EnabledTools []string` field that exists in the schema but is never populated or read by the chat handler.

## Goals / Non-Goals

**Goals:**

- Users can query the knowledge graph conversationally via the existing chat UI
- The LLM can call graph MCP tools during a chat conversation (search, traverse, query entities, etc.)
- Tool invocations are streamed to the client as SSE `mcp_tool` events so users see what the agent did
- Text responses are streamed token-by-token as SSE `token` events (same UX as current chat)
- A `graph-query-agent` definition is available per-project with a curated set of graph tools
- Multi-turn conversations work: the agent retains context from prior turns within a conversation

**Non-Goals:**

- Multi-agent coordination (spawning sub-agents) in chat -- single agent only for now
- Workspace/sandbox provisioning for chat agents -- graph tools don't need sandboxes
- Custom per-user agent definitions in chat -- one default graph-query-agent per project
- Replacing the existing non-agent chat mode -- both modes coexist, selected via `agentDefinitionId`
- Frontend chat UI changes -- those are tracked in the `chat-ui` spec separately

## Decisions

### Decision 1: Streaming callback on AgentExecutor

**Choice:** Add an optional `StreamCallback` to `ExecuteRequest` that the executor invokes for each ADK event during `runner.Run()`.

**Rationale:** The executor's event loop (lines 609-651) already iterates over ADK events. Currently it skips partial events and only persists final content. By adding a callback, the chat handler can receive events in real-time and emit SSE without duplicating the executor's doom loop detection, step tracking, tool call persistence, or other concerns.

**Alternatives considered:**

- _New StreamingAgentExecutor_: Would duplicate all executor logic (callbacks, persistence, doom loop detection). High maintenance burden.
- _Chat handler directly uses ADK runner_: Bypasses all executor features. Would need to re-implement tool call persistence, step limits, and doom loop detection.

**Interface:**

```go
// In ExecuteRequest
type StreamCallback func(event StreamEvent)

type StreamEvent struct {
    Type    StreamEventType // TextDelta, ToolCallStart, ToolCallEnd, Error
    Text    string          // For TextDelta
    Tool    string          // For ToolCallStart/End
    Input   map[string]any  // For ToolCallStart
    Output  map[string]any  // For ToolCallEnd
    Error   string          // For Error
}
```

The executor calls `StreamCallback` at three points:

1. **Partial text events** from `runner.Run()` (currently skipped) -> `TextDelta`
2. **AfterToolCallback** (already exists, line 501) -> `ToolCallStart` before execution, `ToolCallEnd` after
3. **Errors** -> `Error`

The chat handler maps these to SSE events: `TextDelta` -> `token`, `ToolCallStart`/`ToolCallEnd` -> `mcp_tool`.

### Decision 2: Conversation-agent association via `agent_definition_id`

**Choice:** Add `AgentDefinitionID *uuid.UUID` to `kb.chat_conversations` and `AgentDefinitionID *string` to `StreamRequest`.

**Rationale:** A conversation is either a plain RAG chat or an agent-backed chat. This is a persistent property of the conversation (all subsequent messages in that conversation should use the same agent). A single FK captures the full agent config (system prompt, model, tools, max steps).

**Alternatives considered:**

- _Reuse `EnabledTools` field_: Already exists but only stores tool names, not the system prompt, model config, or step limits that an agent definition provides. Would require duplicating agent definition fields onto the conversation.
- _Per-request agent selection (no persistence)_: Would require the client to send the agent ID with every message. Fragile and inconsistent -- a conversation could mix agent and non-agent messages.

**Migration:**

```sql
ALTER TABLE kb.chat_conversations
    ADD COLUMN agent_definition_id UUID REFERENCES kb.agent_definitions(id);
```

### Decision 3: Dual persistence (chat tables + agent tables)

**Choice:** When the chat handler runs an agent-backed conversation, persist to both storage systems:

- `kb.chat_messages` -- the user message and final assistant text response (for the chat UI)
- `kb.agent_run_*` -- the full execution trace including tool calls (via the executor's existing persistence)

**Rationale:** The chat UI reads from `kb.chat_messages` to display conversation history. The agent audit trail in `kb.agent_run_*` provides detailed tool call inspection. Dual writes avoid changing either consumer. The `kb.chat_messages.retrieval_context` field can store a reference to the `agent_run_id` for linking.

**Alternatives considered:**

- _Agent tables only_: Would require rewriting the chat UI's data fetching to read from agent tables for agent-backed conversations. Significant frontend change for minimal benefit.
- _Chat tables only_: Would lose the structured tool call audit trail that `kb.agent_run_tool_calls` provides.

**Flow:**

```
StreamChat (agent-backed):
  1. Persist user message to kb.chat_messages
  2. Create agent run via executor (persists to kb.agent_run_*)
  3. During execution, stream events via SSE (TextDelta -> token, ToolCall -> mcp_tool)
  4. On completion, persist assistant response to kb.chat_messages
     with retrieval_context = {"agent_run_id": "<run-id>"}
```

### Decision 4: Chat handler branching

**Choice:** In `StreamChat`, after resolving the conversation, check for `agent_definition_id`. If present, delegate to a new `streamAgentChat()` method. If absent, use the existing direct-LLM flow unchanged.

**Rationale:** Minimal change to the existing chat flow. The branch point is early and clean -- all agent-specific logic is in a separate method. The existing non-agent chat continues to work identically.

**Sequence (agent-backed):**

```
1. Parse StreamRequest (includes optional agentDefinitionId)
2. Get or create conversation
   - If new + agentDefinitionId provided: set conversation.AgentDefinitionID
   - If existing: use conversation.AgentDefinitionID (ignore request field)
3. Persist user message to kb.chat_messages
4. Start SSE, emit meta event
5. Branch: if conversation.AgentDefinitionID != nil -> streamAgentChat()
   a. Load AgentDefinition from DB
   b. Load conversation history from kb.chat_messages (last N turns)
   c. Build ExecuteRequest with:
      - AgentDefinition
      - User message + conversation history as context
      - StreamCallback that emits SSE events
   d. Call executor.Execute() -- streams via callback
   e. Persist final assistant text to kb.chat_messages
6. Emit done event, close SSE
```

### Decision 5: Conversation history for multi-turn

**Choice:** Before each agent invocation, load the last N messages (default 10) from `kb.chat_messages` and include them in the ADK session as prior context.

**Rationale:** The chat UI already stores all messages in `kb.chat_messages`. Loading them back gives the agent conversation continuity. Using the ADK session service would require mapping between two conversation models. Simpler to build the history from what we already persist.

**Alternative considered:**

- _ADK SessionService for state persistence_: The executor already uses `InMemorySessionService`. Could persist ADK sessions to DB for multi-turn. However, this creates a parallel state store alongside `kb.chat_messages`, and the chat UI wouldn't see ADK session state. Would need a reconciliation layer.

### Decision 6: Default graph-query-agent provisioning

**Choice:** Provide an API endpoint (`POST /api/admin/projects/:projectId/install-default-agents`) that creates default agent definitions for a project. The `graph-query-agent` definition is hardcoded in Go with a curated tool list and system prompt. No database seeding or migration-based provisioning.

**Rationale:** There is no product manifest system (`domain/products/` doesn't exist). The `ProductID` field on `AgentDefinition` is a forward-looking FK with no implementation. Hardcoding the default agent definition in Go keeps it versionable, testable, and deployable without migrations. The admin UI or a setup script calls the endpoint once per project.

**Alternatives considered:**

- _Database migration/seed_: Requires knowing project IDs at migration time. Doesn't work for new projects.
- _Lazy creation on first chat_: Implicit behavior is surprising. Hard to configure before first use.
- _Bootstrap on server start_: Scans all projects and creates missing defaults. Could be slow with many projects and creates definitions users didn't ask for.

**Agent definition:**

```json
{
  "name": "graph-query-agent",
  "description": "Knowledge graph assistant that can search, query, and traverse the project's knowledge graph to answer questions.",
  "system_prompt": "You are a knowledge graph assistant for this project. Use the available tools to answer questions about the knowledge graph -- its entities, relationships, schema, and structure. Always use tools to look up real data rather than guessing. When presenting results, cite the entities and relationships you found. If a search returns no results, say so clearly rather than fabricating an answer.",
  "model": { "name": "gemini-2.0-flash", "temperature": 0.1 },
  "tools": [
    "hybrid_search",
    "query_entities",
    "search_entities",
    "semantic_search",
    "find_similar",
    "get_entity_edges",
    "traverse_graph",
    "list_entity_types",
    "schema_version",
    "list_relationships"
  ],
  "flow_type": "single",
  "is_default": true,
  "max_steps": 15,
  "visibility": "project"
}
```

Low temperature (0.1) because graph querying should be precise, not creative. Max 15 steps allows multi-step queries (search -> get edges -> traverse -> refine) without runaway loops.

## Risks / Trade-offs

**[Latency increase]** Agent-backed chat will be slower than direct LLM streaming because each tool call is a synchronous round-trip (LLM decides tool -> execute tool -> LLM processes result). A 3-tool-call query could take 5-10 seconds vs 1-2 seconds for direct RAG.
-> Mitigation: The `mcp_tool` SSE events provide progress visibility so the user sees activity during tool calls. Low model temperature reduces LLM deliberation time. Step limit of 15 prevents runaway execution.

**[Dual persistence overhead]** Writing to both `kb.chat_messages` and `kb.agent_run_*` doubles the DB writes per conversation turn.
-> Mitigation: Agent run persistence is already async in the executor. Chat message persistence is a single INSERT of the final text. Total overhead is one extra INSERT per turn -- negligible.

**[Context window growth]** Loading 10 turns of conversation history plus tool call results can consume significant context. Tool outputs (e.g., `query_entities` returning 50 entities) can be large.
-> Mitigation: The ADK framework handles context management. Tool results are naturally bounded by the tool's own limits (e.g., `query_entities` has a `limit` parameter). Step limit of 15 bounds total context growth per turn.

**[Agent definition drift]** The hardcoded Go definition may diverge from what users want per project.
-> Mitigation: Users can create custom agent definitions via the existing admin API and set them on conversations. The default is a starting point, not a constraint.

**[No streaming for tool results]** Tool call results arrive as complete JSON, not streamed. Users see "tool started" then wait, then "tool completed" with full results.
-> Mitigation: This is inherent to MCP tool execution. Acceptable for the graph query use case where tool calls complete in <500ms. If needed, a future "tool progress" event type could be added.
