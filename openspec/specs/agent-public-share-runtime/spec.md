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

### Requirement: Share session creation is atomic under the active-session cap

Creating an end-user share session SHALL NOT leave an orphaned backing
`kb.sessions` row behind when the per-(link, end user) active-session cap is
reached. The cap check and the creation of both the backing `kb.sessions` row and
the `kb.agent_share_sessions` join row SHALL happen in one transaction under the
per-(link, end user) advisory lock, with the cap count taken before any insert
and the rows created only when the count is under the cap. When the cap is
reached, no row SHALL be inserted and the request SHALL be denied with 429
`share_session_limit`. The atomic cap guarantee (advisory lock + count) SHALL be
preserved: two concurrent first-session requests can never both create a session
past the cap.

#### Scenario: Cap hit leaves no orphaned session row

- **WHEN** an end user at the active-session cap opens another session
- **THEN** the request is denied with 429 `share_session_limit` and no `kb.sessions` row (and no `kb.agent_share_sessions` row) is inserted

#### Scenario: Under the cap both rows are created

- **WHEN** an end user under the cap opens a session
- **THEN** a `kb.sessions` row and its linked `kb.agent_share_sessions` join row are created in the same transaction
