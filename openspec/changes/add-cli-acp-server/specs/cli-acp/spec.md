## Purpose

The Memory CLI can run as an Agent Client Protocol (ACP) v1 agent over stdio, fronting a single Memory agent for ACP clients (Paseo, Claude Code, Gemini CLI, and others) that spawn a process and speak newline-delimited JSON-RPC 2.0 on stdin/stdout.

## ADDED Requirements

### Requirement: ACP stdio command

The CLI SHALL provide a `memory acp` command that runs the process as an ACP v1 agent over stdio. It SHALL write only ACP JSON-RPC messages to stdout and route diagnostics to stderr.

#### Scenario: Command available

- **WHEN** a user runs `memory acp --help`
- **THEN** the command is listed under the `ai` command group with a `--agent` flag

#### Scenario: Missing agent selection

- **WHEN** `memory acp` runs with neither `--agent <skill-id>` nor the `MEMORY_AGENT` environment variable set
- **THEN** the command exits non-zero with an error explaining how to select an agent

### Requirement: initialize handshake

The CLI SHALL answer the ACP `initialize` method with `protocolVersion: 1`, an honest `agentCapabilities` object (all optional capabilities false), and `agentInfo` naming the agent `memory`.

#### Scenario: Initialize response

- **WHEN** the client sends `initialize`
- **THEN** the response carries `protocolVersion` 1, `agentCapabilities.loadSession` false, and `agentInfo.name` `"memory"`

### Requirement: Session and prompt bridge

The CLI SHALL answer `session/new` with a fresh `sessionId`, and `session/prompt` by running one Memory agent turn via A2A `message:send` using the selected skill id, emitting a `session/update` `agent_message_chunk` notification with the reply text, and returning `stopReason` `"end_turn"`.

#### Scenario: Prompt forwards to the selected agent

- **WHEN** the client sends `session/prompt` with text content
- **THEN** the CLI sends an A2A message carrying `metadata.skillId` equal to the selected skill id and the prompt text as a single text part

#### Scenario: Reply is streamed back

- **WHEN** the Memory agent turn completes with reply text
- **THEN** the CLI emits a `session/update` notification whose update is `agent_message_chunk` with the reply text, followed by a `session/prompt` response with `stopReason` `"end_turn"`

#### Scenario: Empty prompt

- **WHEN** the client sends `session/prompt` with no text content
- **THEN** the CLI returns `stopReason` `"end_turn"` without making a backend call

### Requirement: Multi-turn threading

The CLI SHALL thread consecutive prompts within a session by carrying the A2A `contextId` returned by a prior turn on the next turn's message.

#### Scenario: Context carried across turns

- **WHEN** a prompt returns an A2A task with a non-empty `contextId`, and a later prompt arrives on the same session
- **THEN** the later A2A message carries that `contextId`

### Requirement: Cancellation

The CLI SHALL handle the `session/cancel` notification by interrupting any in-flight A2A request for that session and resolving the pending `session/prompt` with `stopReason` `"cancelled"`.

#### Scenario: Cancel interrupts a running turn

- **WHEN** a `session/cancel` notification arrives while a `session/prompt` for that session is in flight
- **THEN** the in-flight request is aborted and the prompt resolves with `stopReason` `"cancelled"`

### Requirement: Unknown methods

The CLI SHALL return a JSON-RPC error for unknown methods and ignore unknown notifications.

#### Scenario: Unknown method

- **WHEN** the client sends a method the CLI does not implement
- **THEN** the CLI returns a JSON-RPC error response carrying the request id
