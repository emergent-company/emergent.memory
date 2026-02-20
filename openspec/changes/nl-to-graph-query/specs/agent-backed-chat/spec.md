## ADDED Requirements

### Requirement: Agent-backed conversation creation

The chat system SHALL support creating conversations that are backed by an agent definition, enabling tool-calling capabilities during interactive chat.

#### Scenario: Create agent-backed conversation

- **WHEN** a client sends a `StreamRequest` with an `agentDefinitionId` field and no `conversationId`
- **THEN** the system SHALL create a new conversation in `kb.chat_conversations`
- **AND** the conversation SHALL have `agent_definition_id` set to the provided value
- **AND** all subsequent messages in this conversation SHALL be processed by the agent executor

#### Scenario: Continue existing agent-backed conversation

- **WHEN** a client sends a `StreamRequest` with a `conversationId` for an existing agent-backed conversation
- **THEN** the system SHALL use the conversation's stored `agent_definition_id`
- **AND** the system SHALL ignore any `agentDefinitionId` in the request body

#### Scenario: Create plain conversation (backward compatible)

- **WHEN** a client sends a `StreamRequest` without an `agentDefinitionId`
- **THEN** the system SHALL create a conversation with `agent_definition_id` set to NULL
- **AND** the conversation SHALL use the existing direct-LLM RAG flow unchanged

#### Scenario: Invalid agent definition ID

- **WHEN** a client sends a `StreamRequest` with an `agentDefinitionId` that does not exist in `kb.agent_definitions`
- **THEN** the system SHALL return a 400 error with a message indicating the agent definition was not found
- **AND** the system SHALL NOT create a conversation

### Requirement: Agent executor streaming callback

The agent executor SHALL support an optional streaming callback that emits events during execution, enabling real-time SSE streaming for interactive use cases.

#### Scenario: StreamCallback receives text deltas

- **WHEN** the agent executor processes a run with a `StreamCallback` set on the `ExecuteRequest`
- **AND** the ADK runner emits a partial text event
- **THEN** the executor SHALL invoke the callback with a `StreamEvent` of type `TextDelta`
- **AND** the `Text` field SHALL contain the incremental text token

#### Scenario: StreamCallback receives tool call events

- **WHEN** the agent executor's `AfterToolCallback` fires after a tool invocation
- **THEN** the executor SHALL invoke the `StreamCallback` with a `StreamEvent` of type `ToolCallStart` before execution (with `Tool` name and `Input`)
- **AND** the executor SHALL invoke the callback with a `StreamEvent` of type `ToolCallEnd` after execution (with `Tool` name, `Output`, and optionally `Error`)

#### Scenario: StreamCallback receives errors

- **WHEN** the agent executor encounters an error during execution
- **THEN** the executor SHALL invoke the callback with a `StreamEvent` of type `Error`
- **AND** the `Error` field SHALL contain a description of the failure

#### Scenario: No StreamCallback (backward compatible)

- **WHEN** the agent executor processes a run without a `StreamCallback` (nil)
- **THEN** the executor SHALL behave identically to the current batch mode
- **AND** no streaming events SHALL be emitted

### Requirement: Chat handler agent branching

The `StreamChat` handler SHALL branch between direct-LLM and agent-backed flows based on the conversation's `agent_definition_id`.

#### Scenario: Agent-backed streaming flow

- **WHEN** a user sends a message to an agent-backed conversation
- **THEN** the handler SHALL persist the user message to `kb.chat_messages`
- **AND** the handler SHALL start SSE and emit a `meta` event
- **AND** the handler SHALL load the `AgentDefinition` from the database
- **AND** the handler SHALL load conversation history (last N messages from `kb.chat_messages`)
- **AND** the handler SHALL call the agent executor with a `StreamCallback`
- **AND** the handler SHALL map `TextDelta` events to SSE `token` events
- **AND** the handler SHALL map `ToolCallStart`/`ToolCallEnd` events to SSE `mcp_tool` events
- **AND** the handler SHALL persist the final assistant text to `kb.chat_messages` on completion
- **AND** the handler SHALL emit a `done` SSE event

#### Scenario: Direct-LLM flow unchanged

- **WHEN** a user sends a message to a plain (non-agent) conversation
- **THEN** the handler SHALL use the existing direct Vertex AI streaming flow
- **AND** behavior SHALL be identical to the current implementation

### Requirement: SSE event streaming for tool calls

The agent-backed chat flow SHALL emit `mcp_tool` SSE events so clients can display tool call progress in real-time.

#### Scenario: Tool call started event

- **WHEN** the agent begins executing an MCP tool during a chat conversation
- **THEN** the system SHALL emit an SSE event with `type: "mcp_tool"`, `tool: "<tool_name>"`, and `status: "started"`

#### Scenario: Tool call completed event

- **WHEN** the agent finishes executing an MCP tool successfully
- **THEN** the system SHALL emit an SSE event with `type: "mcp_tool"`, `tool: "<tool_name>"`, `status: "completed"`, and `result` containing the tool output

#### Scenario: Tool call error event

- **WHEN** an MCP tool execution fails during a chat conversation
- **THEN** the system SHALL emit an SSE event with `type: "mcp_tool"`, `tool: "<tool_name>"`, `status: "error"`, and `error` containing the error message
- **AND** the agent SHALL continue execution (tool errors are non-fatal to the conversation)

### Requirement: Dual persistence for agent-backed conversations

Agent-backed conversations SHALL persist data to both `kb.chat_messages` (for UI) and `kb.agent_run_*` (for audit trail).

#### Scenario: User message persisted to chat tables

- **WHEN** a user sends a message to an agent-backed conversation
- **THEN** the system SHALL persist the message to `kb.chat_messages` with `role: "user"`

#### Scenario: Assistant response persisted to chat tables

- **WHEN** the agent completes its response
- **THEN** the system SHALL persist the final text response to `kb.chat_messages` with `role: "assistant"`
- **AND** the `retrieval_context` field SHALL contain `{"agent_run_id": "<run-id>"}` linking to the full agent trace

#### Scenario: Agent execution trace persisted to agent tables

- **WHEN** the agent executor runs during a chat conversation
- **THEN** the executor SHALL persist all messages to `kb.agent_run_messages` (via existing persistence)
- **AND** the executor SHALL persist all tool calls to `kb.agent_run_tool_calls` (via existing persistence)

### Requirement: Multi-turn conversation context

Agent-backed conversations SHALL include prior conversation history so the agent can reference earlier turns.

#### Scenario: Load conversation history for agent

- **WHEN** the chat handler prepares an agent execution for a conversation with prior messages
- **THEN** the system SHALL load the last N messages (default 10) from `kb.chat_messages` for that conversation
- **AND** the messages SHALL be included in the ADK session as prior context

#### Scenario: First message in conversation

- **WHEN** the chat handler prepares an agent execution for a new conversation (no prior messages)
- **THEN** the system SHALL pass only the current user message to the agent
- **AND** the agent SHALL execute without prior context

#### Scenario: Conversation history respects turn limit

- **WHEN** a conversation has more than N messages
- **THEN** the system SHALL load only the most recent N messages (ordered chronologically)
- **AND** older messages SHALL NOT be included in the agent context

### Requirement: Database migration for agent-backed conversations

The system SHALL add an `agent_definition_id` foreign key to `kb.chat_conversations`.

#### Scenario: Migration adds column

- **WHEN** the migration runs
- **THEN** `kb.chat_conversations` SHALL have a nullable `agent_definition_id` column of type `UUID`
- **AND** the column SHALL reference `kb.agent_definitions(id)`

#### Scenario: Existing conversations unaffected

- **WHEN** the migration runs on a database with existing conversations
- **THEN** all existing conversations SHALL have `agent_definition_id` set to NULL
- **AND** they SHALL continue to function as plain (non-agent) conversations
