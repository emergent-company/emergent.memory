## ADDED Requirements

### Requirement: Share chat renders through the shared chat engine

The public share page (`/share/agent`) SHALL render its conversation with the same shared engine the authenticated chat workspace uses (`chat-components.js`, `chat-host.js`, `chat-stream.js`), not a page-specific renderer. Message bubbles, markdown enhancement (per-message and per-code-block copy), tool-call chips, thinking blocks, structured (A2UI) cards, the citations/Sources footer, and the in-bubble typing indicator SHALL be produced by the shared components so the two surfaces are pixel-identical for the same input.

#### Scenario: Shared components on the share page
- **WHEN** a share-page turn renders a reply, a tool call, a thinking block, and citations
- **THEN** the DOM is produced by `MemoryChatComponents`/`MemoryChatStream` and matches the `/chat` workspace rendering for the same frames

#### Scenario: No page-local bubble renderer
- **WHEN** the share page renders history or a live stream
- **THEN** it does not use a share-specific bubble builder; bubbles come from the shared engine

#### Scenario: Typing renders in the assistant bubble
- **WHEN** the share page is awaiting the first streamed token
- **THEN** the typing indicator renders inside the assistant bubble (as on `/chat`), not as a separate dock element

### Requirement: Share live stream carries and dispatches the full frame set

The share page SHALL dispatch the same SSE frame types as the authenticated chat workspace — `token`, `thinking`, `mcp_tool`, `question`, `approval`, `citations`, structured-UI, live-replay, `error`, and `done` — through the shared dispatcher, so each renders identically on both surfaces. The share SSE wire SHALL use the same frame field names as `/api/chat` (a `token` frame carries `token`), and SHALL retain the leading session `meta` frame.

#### Scenario: Tool calls and thinking stream on the share page
- **WHEN** a share run emits `mcp_tool` and `thinking` frames
- **THEN** the share page renders the same tool chips and thinking blocks as `/chat`

#### Scenario: Citations render as Sources
- **WHEN** a share run emits a `citations` frame
- **THEN** the share page renders the same Sources block as `/chat`

#### Scenario: Wire shape matches the authenticated stream
- **WHEN** the gateway proxies a share stream
- **THEN** token frames carry the `token` field (not a share-only `delta` field) and the `meta` session frame is preserved

### Requirement: Share history renders the full run timeline

The share session transcript SHALL return the full run timeline (all item kinds — user/assistant messages, tool calls with input/output, run markers, reasoning, citations), with answered `ask_user` calls annotated with the end user's response, and the gateway SHALL render it through the same history path as `/api/chat` (markdown sanitization and inline pending-approval cards).

#### Scenario: Resuming a session renders the full timeline
- **WHEN** an end user reopens a share session that ran tools
- **THEN** its tool chips, thinking blocks, and citations render exactly as they would on `/chat`, not as plain text

#### Scenario: Transcript stays scoped to the caller
- **WHEN** a transcript is requested for a session the caller's end-user ref does not own
- **THEN** the request fails as not-found and no other end user's transcript is returned

#### Scenario: Raw tool I/O included
- **WHEN** a timeline tool-call item carries `tool_input`/`tool_output`
- **THEN** those values are included in the share transcript (full parity on this surface)

#### Scenario: Composed system instruction excluded
- **WHEN** the timeline carries the run's composed system instruction (the owner's system prompt + skills appendix, persisted as a `role:"system"` message)
- **THEN** it is stripped from the share transcript and never rendered on the public page, even though the authenticated chat shows it

### Requirement: Inline question and approval cards on the share page

Pending tool approvals and `ask_user` questions SHALL render on the share page as the same inline cards the authenticated chat uses, offering approve, reject-with-reason, and cancel, and posting to share decision routes.

#### Scenario: Approve and reject-with-reason
- **WHEN** an end user approves or rejects a pending tool call with a reason
- **THEN** the share decision route forwards the decision and reason and the run resumes

#### Scenario: Cancel
- **WHEN** an end user cancels a pending approval
- **THEN** the share cancel route marks the question cancelled and resumes the run with the call treated as not taken

#### Scenario: On-load pending approvals match the admin card
- **WHEN** the transcript loads with a pending approval
- **THEN** the card shows the tool name and args and matches the admin chat's approval card

### Requirement: Share session rail matches the authenticated rail

The share session rail SHALL use the authenticated rail's server-rendered fragment and row markup, support the archived filter, and offer archive, unarchive, and delete of the end user's own sessions.

#### Scenario: Rail actions
- **WHEN** an end user archives, unarchives, or deletes one of their sessions
- **THEN** the corresponding share route performs the action and the rail refreshes

#### Scenario: Archived filter
- **WHEN** the end user toggles the archived filter
- **THEN** archived sessions are included or excluded, matching the authenticated rail

### Requirement: Public-share guardrails retained with full parity

Full chat parity SHALL NOT weaken the public-share guardrails: the sealed-cookie key exchange, signed end-user ref, edge rate limiting, per-link budget, `show_session_list`/`allow_end_user_approvals` gating, the reserved `share:agent-chat` scope bound to `/api/share/agent`, and the share security headers all remain in force. `allow_end_user_approvals` gates every end-user approval decision, including cancel (a cancel resumes the run). Exposing raw agent tool input/output on this surface is deliberate and documented; the composed system instruction is not exposed.

#### Scenario: Guardrails still apply
- **WHEN** the share page renders the full chat
- **THEN** the cookie exchange, signed ref, rate limit, budget, config gating, scope binding, and security headers are unchanged

#### Scenario: Tool I/O exposure is deliberate
- **WHEN** the share transcript includes raw tool input/output
- **THEN** this is an accepted consequence of full parity, not an omission
