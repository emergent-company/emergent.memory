## Purpose

Corrects the owner-facing share transcript for the unarchived `agent-public-share` change: the project-scoped owner transcript endpoint MUST return the agent's replies whatever author role the runtime persisted, and the owner transcript page MUST render markdown through the live `.memory-md` wrapper.

## ADDED Requirements

### Requirement: Owner transcript returns the agent's replies regardless of the persisted author role
The project-scoped owner transcript endpoint `GET /api/projects/:projectId/share-sessions/:id` SHALL return the agent's reply messages whatever author role the executor persisted them under. The executor persists assistant turns under the ADK event Author — the sanitized agent name (e.g. `coder`), not the literal `assistant` — so the transcript MUST map any author role that is not `user`, `tool`, `tool_result`, or `system` (including `assistant` and any sanitized agent name) to the wire role `assistant`. The transcript SHALL remain ordered, SHALL remain scoped to the project via the session's link (404 when the session belongs to another project or does not exist), SHALL remain plain text, and SHALL keep the wire shape `{role: "user" | "assistant", content}` unchanged.

#### Scenario: Agent reply persisted under the sanitized agent name is returned
- **WHEN** an owner fetches a project share session whose agent turns were persisted with author role `coder`
- **THEN** those turns are returned as `role: "assistant"` messages in transcript order

#### Scenario: Tool and tool_result roles are never surfaced
- **WHEN** the session history contains `tool` or `tool_result` role messages
- **THEN** they are excluded from the transcript, so raw tool output never leaks to the owner

#### Scenario: System and empty text are dropped
- **WHEN** the session history contains a `system` role message or any message with empty text
- **THEN** they are excluded from the transcript

#### Scenario: Ordering and project scoping are preserved
- **WHEN** an owner fetches an own-project session's transcript
- **THEN** the returned `messages` array preserves the persisted order, and a session whose link belongs to a different project (or does not exist) is denied with 404

### Requirement: Owner transcript page renders markdown through the live markdown wrapper
The owner-facing transcript page (the share-session transcript and the session timeline) SHALL render user and assistant message markdown through the `.memory-md` wrapper, never the stale `alfred-md` class, so the shared markdown styles apply.

#### Scenario: Markdown uses the live wrapper
- **WHEN** the owner transcript page renders a message's markdown
- **THEN** the markdown container carries the `memory-md` class and no `alfred-md` class, so it is styled by the shared markdown stylesheet
