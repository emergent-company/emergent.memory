## 1. Database Migration

- [x] 1.1 Create Goose migration adding nullable `agent_definition_id UUID REFERENCES kb.agent_definitions(id)` column to `kb.chat_conversations`
- [x] 1.2 Update `Conversation` struct in `domain/chat/entity.go` to include `AgentDefinitionID *uuid.UUID` field with bun tag
- [x] 1.3 Add `AgentDefinitionID *string` field to `StreamRequest` in `domain/chat/entity.go`
- [x] 1.4 Run migration and verify existing conversations have `agent_definition_id = NULL`

## 2. Agent Executor Streaming Callback

- [x] 2.1 Define `StreamEvent` struct and `StreamEventType` enum (`TextDelta`, `ToolCallStart`, `ToolCallEnd`, `Error`) in `domain/agents/executor.go`
- [x] 2.2 Add optional `StreamCallback func(StreamEvent)` field to `ExecuteRequest`
- [x] 2.3 Modify the `runner.Run()` event loop to emit `TextDelta` events on partial text content when `StreamCallback` is set (currently partial events are skipped)
- [x] 2.4 Modify `AfterToolCallback` to emit `ToolCallStart` (before tool execution) and `ToolCallEnd` (after) via `StreamCallback` when set
- [x] 2.5 Emit `Error` stream events on executor errors when `StreamCallback` is set
- [x] 2.6 Verify nil `StreamCallback` produces identical behavior to current batch mode (no regressions)
- [ ] 2.7 Write unit tests for `StreamCallback` event emission (text deltas, tool calls, errors, nil callback)

## 3. Chat Handler Agent Branching

- [x] 3.1 In `StreamChat` conversation creation path: if `StreamRequest.AgentDefinitionID` is set, validate it exists in `kb.agent_definitions`, set it on the new conversation
- [x] 3.2 In `StreamChat` existing conversation path: load `AgentDefinitionID` from the conversation, ignore request body field
- [x] 3.3 Add branch after SSE meta event: if `conversation.AgentDefinitionID != nil`, call new `streamAgentChat()` method; otherwise continue existing direct-LLM flow
- [x] 3.4 Implement `streamAgentChat()` method on chat handler:
  - Load `AgentDefinition` from DB via agent service
  - Load last 10 messages from `kb.chat_messages` for conversation history
  - Build `ExecuteRequest` with agent definition, user message, history, and `StreamCallback`
  - Map `TextDelta` -> SSE `token` event, `ToolCallStart`/`ToolCallEnd` -> SSE `mcp_tool` event
  - On completion, persist final assistant text to `kb.chat_messages` with `retrieval_context = {"agent_run_id": "<id>"}`
- [x] 3.5 Inject agent executor dependency into chat handler (add to handler struct and constructor)
- [x] 3.6 Verify existing non-agent chat flow is completely unchanged (run existing chat tests)

## 4. Default Graph-Query-Agent Definition

- [x] 4.1 Add `InstallDefaultAgents(ctx, projectID)` method to agent service that creates the `graph-query-agent` definition with hardcoded config (name, description, system prompt, tools, model, flow_type, max_steps, visibility, is_default)
- [x] 4.2 Make installation idempotent: check for existing `graph-query-agent` by name+project before creating
- [x] 4.3 Add `POST /api/admin/projects/:projectId/install-default-agents` handler that calls the service method
- [x] 4.4 Register the new route in the admin router
- [ ] 4.5 Write E2E test: install default agents, verify graph-query-agent definition created with correct tools and config
- [ ] 4.6 Write E2E test: call install endpoint twice, verify idempotent (no duplicate)

## 5. Integration Testing

- [ ] 5.1 Write E2E test: create agent-backed conversation by sending `StreamRequest` with `agentDefinitionId`, verify conversation has `agent_definition_id` set
- [ ] 5.2 Write E2E test: send message to agent-backed conversation, verify SSE stream contains `mcp_tool` events and `token` events
- [ ] 5.3 Write E2E test: send message to agent-backed conversation, verify assistant response persisted to `kb.chat_messages` with `agent_run_id` in `retrieval_context`
- [ ] 5.4 Write E2E test: send message to agent-backed conversation, verify agent execution trace persisted to `kb.agent_run_messages` and `kb.agent_run_tool_calls`
- [ ] 5.5 Write E2E test: send invalid `agentDefinitionId`, verify 400 error and no conversation created
- [ ] 5.6 Write E2E test: multi-turn agent conversation (2+ messages), verify agent receives prior history and responds with context awareness
- [ ] 5.7 Verify existing plain chat E2E tests still pass (no regressions)

## 6. Frontend: Chat UI Agent Support

- [ ] 6.1 Update `StreamRequest` type in frontend to include optional `agentDefinitionId` field
- [ ] 6.2 Add SSE event parsing for `mcp_tool` event type in the chat streaming hook
- [ ] 6.3 Create `ToolCallIndicator` component: shows tool name with loading/completed/error states
- [ ] 6.4 Update chat message display to interleave `ToolCallIndicator` components between text blocks based on `mcp_tool` events
- [ ] 6.5 Add collapsible detail view to completed tool calls showing the tool result data
- [ ] 6.6 Add agent-backed conversation indicator showing agent name when conversation has `agent_definition_id`
- [ ] 6.7 Add UI mechanism to start a new agent-backed conversation (sends `agentDefinitionId` in first message)

## 7. Documentation and Cleanup

- [ ] 7.1 Add deprecation note to `docs/integrations/mcp/MCP_CHAT_INTEGRATION_DESIGN.md` marking Phases 2-3 as superseded by agent-backed chat
- [ ] 7.2 Update `apps/server-go/AGENT.md` with agent-backed chat patterns and the new `StreamCallback` API
- [ ] 7.3 Update `apps/admin/src/hooks/AGENT.md` if new hooks are created for agent chat
