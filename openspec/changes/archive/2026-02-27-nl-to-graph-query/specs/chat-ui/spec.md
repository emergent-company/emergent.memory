## ADDED Requirements

### Requirement: Agent-backed conversation initiation

The chat UI SHALL allow users to start agent-backed conversations by selecting an agent definition, enabling tool-calling capabilities in the chat interface.

#### Scenario: Start conversation with graph-query-agent

- **WHEN** a user starts a new chat conversation with an agent-backed mode selected
- **THEN** the UI SHALL include `agentDefinitionId` in the `StreamRequest` body
- **AND** the conversation SHALL be marked as agent-backed for all subsequent messages

#### Scenario: Visual indicator for agent-backed conversation

- **WHEN** a conversation is backed by an agent definition
- **THEN** the UI SHALL display an indicator showing the agent name (e.g., "Knowledge Graph Assistant")
- **AND** the indicator SHALL distinguish agent-backed conversations from plain chat conversations

### Requirement: MCP tool call SSE event rendering

The chat UI SHALL render `mcp_tool` SSE events inline during agent-backed conversations, showing users which graph operations the agent is performing.

#### Scenario: Display tool call started

- **WHEN** the SSE stream emits an event with `type: "mcp_tool"` and `status: "started"`
- **THEN** the UI SHALL display a loading indicator with the tool name (e.g., "Searching entities...")
- **AND** the indicator SHALL appear inline in the message stream below any preceding text tokens

#### Scenario: Display tool call completed

- **WHEN** the SSE stream emits an event with `type: "mcp_tool"` and `status: "completed"`
- **THEN** the UI SHALL replace the loading indicator with a completed state showing the tool name
- **AND** the tool result SHALL be available in a collapsible detail view
- **AND** the detail view SHALL render the result data in a readable format

#### Scenario: Display tool call error

- **WHEN** the SSE stream emits an event with `type: "mcp_tool"` and `status: "error"`
- **THEN** the UI SHALL display the tool name with an error state
- **AND** the error message SHALL be visible to the user
- **AND** the conversation SHALL continue (tool errors are non-fatal)

#### Scenario: Multiple sequential tool calls

- **WHEN** the agent makes multiple tool calls in a single response (e.g., search then traverse)
- **THEN** the UI SHALL display each tool call as a separate inline element in chronological order
- **AND** text tokens between tool calls SHALL be displayed normally

### Requirement: Mixed token and tool event streaming

The chat UI SHALL correctly interleave text tokens and tool call events in the message display during agent-backed conversations.

#### Scenario: Text before and after tool calls

- **WHEN** the agent streams text tokens, then makes a tool call, then streams more text
- **THEN** the UI SHALL display: text block -> tool call indicator -> text block
- **AND** the final message SHALL read as a coherent response with tool calls shown inline

#### Scenario: Streaming text tokens in agent mode

- **WHEN** the SSE stream emits `token` events during an agent-backed conversation
- **THEN** the UI SHALL render them identically to the existing non-agent chat streaming behavior
- **AND** tokens SHALL appear incrementally in real-time
