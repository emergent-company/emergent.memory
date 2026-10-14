# agent-public-share-runtime Specification

## Purpose
Corrects the end-user share transcript for the unarchived `agent-public-share` change: the ref-scoped end-user transcript endpoint MUST return the agent's replies whatever author role the runtime persisted, so a resumed anonymous session renders the agent's answers and not just the end-user's questions.

## Requirements

### Requirement: End-user session transcript returns the agent's replies regardless of the persisted author role
The end-user transcript endpoint `GET /api/share/agent/sessions/:id` SHALL return the agent's reply messages whatever author role the executor persisted them under. The executor persists assistant turns under the ADK event Author — the sanitized agent name, not the literal `assistant` — so the transcript MUST map any author role that is not `user`, `tool`, `tool_result`, or `system` (including `assistant` and any sanitized agent name) to the wire role `assistant`. The transcript SHALL remain ordered, SHALL remain scoped to the caller's verified `end_user_ref`, SHALL remain plain text, and SHALL keep the wire shape `{id, title, isArchived, messages: [{role: "user" | "assistant", content}], createdAt}` unchanged.

#### Scenario: Agent reply persisted under the sanitized agent name is returned
- **WHEN** an end user fetches one of their own sessions whose agent turns were persisted with author role `coder`
- **THEN** those turns are returned as `role: "assistant"` messages in transcript order

#### Scenario: Tool, tool_result, and system roles are never surfaced
- **WHEN** the session history contains `tool`, `tool_result`, or `system` role messages, or any message with empty text
- **THEN** they are excluded from the transcript

#### Scenario: Ref scoping is preserved
- **WHEN** an end user fetches a session id not owned by their verified ref on the link
- **THEN** the request is denied and no transcript leaks
