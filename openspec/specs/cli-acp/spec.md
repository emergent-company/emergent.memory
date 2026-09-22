# cli-acp Specification

## Purpose
The Memory CLI can run as an Agent Client Protocol (ACP) v1 agent over stdio, fronting a single Memory agent for ACP clients (Paseo, Claude Code, Gemini CLI, and others) that spawn a process and speak newline-delimited JSON-RPC 2.0 on stdin/stdout.

## Requirements

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

The CLI SHALL answer `session/new` with a fresh `sessionId`, and `session/prompt` by running one Memory agent turn via A2A `message:stream` using the selected skill id, and returning `stopReason` `"end_turn"` when the turn completes.

#### Scenario: Prompt forwards to the selected agent

- **WHEN** the client sends `session/prompt` with text content
- **THEN** the CLI sends an A2A `message:stream` request carrying `metadata.skillId` equal to the selected skill id and the prompt text as a single text part

#### Scenario: Empty prompt

- **WHEN** the client sends `session/prompt` with no text content
- **THEN** the CLI returns `stopReason` `"end_turn"` without making a backend call

### Requirement: Streaming output

The CLI SHALL stream the agent's reply as incremental `session/update` `agent_message_chunk` notifications as A2A emits text deltas, deduplicating the repeated full text carried by artifact/message/task events.

#### Scenario: Reply is streamed incrementally

- **WHEN** the Memory agent turn streams text deltas
- **THEN** the CLI emits one `agent_message_chunk` notification per new text suffix, followed by a `session/prompt` response with `stopReason` `"end_turn"`

#### Scenario: Duplicate full text is not re-emitted

- **WHEN** A2A re-emits the full accumulated text (terminal message, last-chunk artifact, or final task snapshot)
- **THEN** the CLI does not emit a duplicate `agent_message_chunk` for already-streamed text

### Requirement: Human-in-the-loop resume

The CLI SHALL surface a paused turn's question when A2A reports `TASK_STATE_INPUT_REQUIRED`, remember the task id, and resume it on the next prompt in the same session by sending the A2A message with that `taskId`.

#### Scenario: Pause surfaces the question

- **WHEN** an A2A turn pauses at `TASK_STATE_INPUT_REQUIRED`
- **THEN** the CLI emits an `agent_message_chunk` with the question text and returns `stopReason` `"end_turn"`, recording the task id

#### Scenario: Next prompt resumes the paused task

- **WHEN** a later `session/prompt` arrives on the same session after a pause
- **THEN** the CLI sends an A2A `message:stream` request carrying `message.taskId` equal to the paused task id (without `metadata.skillId`)

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

### Requirement: Failure reporting

The CLI SHALL report a failed or rejected Memory turn as `stopReason` `"refusal"` (and a server-cancelled turn as `"cancelled"`), rather than `"end_turn"`, so ACP clients can distinguish failure from success.

#### Scenario: Failed turn

- **WHEN** the A2A stream reports `TASK_STATE_FAILED` or `TASK_STATE_REJECTED`
- **THEN** the CLI streams any failure message and returns a `session/prompt` response with `stopReason` `"refusal"`
