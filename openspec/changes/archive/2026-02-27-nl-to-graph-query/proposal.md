## Why

Users cannot query the knowledge graph conversationally. The chat service (`domain/chat/`) does a single RAG pass -- it searches, injects context, and generates -- but the LLM cannot refine its search, traverse relationships, or compose multi-step graph queries. Meanwhile, the full agent infrastructure (`domain/agents/`) with 30+ MCP graph tools and ADK-based execution already exists, but is only available to background agents, not interactive chat.

The gap is the connection: wiring the chat's interactive streaming endpoint to use the agent executor (or a graph-query-agent definition) so the LLM can use graph tools during conversation. This replaces the custom NL-to-query translation pipeline proposed in the MCP Chat Integration Design with a simpler, more powerful approach: LLM tool calling IS the query translator.

## What Changes

- **New `graph-query-agent` definition** shipped as a default agent for the `emergent.memory` product. Configured with graph/search MCP tools (`hybrid_search`, `query_entities`, `traverse_graph`, `get_entity_edges`, `search_entities`, `semantic_search`, `find_similar`, `list_entity_types`, `schema_version`). The LLM decides which tools to call based on the user's natural language question.
- **Agent-backed chat mode**: The chat streaming endpoint gains the ability to use an agent executor instead of direct LLM calls when a conversation is associated with a graph-query-agent. This gives the LLM access to graph tools during the conversation, enabling multi-step queries (search -> get edges -> traverse -> refine).
- **Iterative graph exploration**: Unlike the current single-shot RAG, the agent can chain multiple tool calls per turn -- e.g., "find all Decisions" -> `query_entities(type=Decision)` -> "what are they related to?" -> `get_entity_edges(...)` -> `traverse_graph(...)`. The LLM composes these naturally.
- **Deprecates MCP Chat Integration Design**: The custom keyword-based tool detector, tool router, and graph query translator pipeline (`docs/integrations/mcp/MCP_CHAT_INTEGRATION_DESIGN.md` Phases 2-3) is superseded. LLM tool calling replaces intent classification. The ToolPool replaces tool routing. Agent state persistence replaces custom conversation history.

## Capabilities

### New Capabilities

- `graph-query-agent`: Default agent definition for natural language graph querying, including tool whitelist, system prompt, and model configuration. Shipped with `emergent.memory` product.
- `agent-backed-chat`: Chat streaming mode that routes through the agent executor instead of direct LLM calls, giving the LLM access to MCP tools during interactive conversation. Includes tool call visibility in the SSE stream.

### Modified Capabilities

- `chat-ui`: Chat UI needs to render tool invocations inline (the agent will emit tool call events that should be visible to the user, showing which graph operations were performed).
- `unified-search`: No requirement changes, but the graph-query-agent will be the primary consumer of the unified search API via the `hybrid_search` MCP tool.

## Impact

**Backend (`apps/server-go/`)**:

- `domain/chat/handler.go` -- StreamChat needs a branch: if conversation uses an agent, route through `agents.Executor` instead of `vertex.Client.GenerateStreaming()`
- `domain/chat/entity.go` -- Conversation may need an `agent_definition_id` field to associate with a graph-query-agent
- `domain/agents/executor.go` -- May need a streaming mode that emits SSE-compatible events (currently returns final result)
- `domain/mcp/service.go` -- No changes needed; tools already exist

**Frontend (`apps/admin/`)**:

- Chat components need to handle tool call events in the SSE stream
- Display tool invocations as collapsible UI elements showing what the agent did

**Database**:

- Seed data for default `graph-query-agent` definition in `kb.agent_definitions`
- Possible migration to add `agent_definition_id` to `kb.chat_conversations`

**Deprecated**:

- `docs/integrations/mcp/MCP_CHAT_INTEGRATION_DESIGN.md` Phases 2-3 (tool detector, tool router, graph query translator) -- superseded by this approach
