## Purpose

Defines the server-side runtime for anonymous end users of a public share link: the cryptographically signed `X-End-User-Ref` identity, join-table session ownership with per-user visibility, metadata-only email capture, anonymous tool approvals, the sandbox gate, the suspended-run reaper, and the transcript endpoint.

## ADDED Requirements

### Requirement: End-user identity is a signed reference verified constant-time
An anonymous end user SHALL be identified by a pair of headers: `X-End-User-Ref` (a UUID v4 reference minted by the gateway) and `X-End-User-Ref-Sig` = base64url-nopad(HMAC-SHA256(key=`SHARE_REF_SECRET`, message=the exact `end_user_ref`)). The server SHALL verify the signature with constant-time comparison. A missing ref or signature, or an invalid signature, SHALL return 401; an unconfigured secret SHALL fail closed with 503. The reference SHALL NOT be accepted from the request body or query string.

#### Scenario: Signature is verified constant-time
- **WHEN** a share request carries `X-End-User-Ref` and `X-End-User-Ref-Sig`
- **THEN** the server recomputes HMAC-SHA256 over the ref and compares it to the signature in constant time before trusting the ref

#### Scenario: Missing or invalid signature is rejected
- **WHEN** the ref or signature header is missing, or the signature does not match
- **THEN** the request is denied with 401 and the ref is never trusted

#### Scenario: Unconfigured secret fails closed
- **WHEN** `SHARE_REF_SECRET` is unset
- **THEN** identity-bearing share routes return 503 and never accept an unsigned ref

#### Scenario: Body/query ref is never accepted
- **WHEN** a request includes an `endUserRef` in the body or query string
- **THEN** it is ignored; the identity comes only from the signed headers

#### Scenario: Ref must exist for the link
- **WHEN** an identity-bearing request carries a validly signed ref that has no `kb.agent_share_end_users` row for the bound link
- **THEN** the request is denied (404) so existence never leaks

### Requirement: Session ownership uses a join table with per-end-user visibility
Session ownership SHALL be recorded in a join table `kb.agent_share_sessions` linking a share link to an `kb.acp_sessions` row, keyed by `end_user_ref` — not via nullable columns on `kb.acp_sessions`. Each end user SHALL see only their own sessions. The system SHALL enforce a maximum of 5 active sessions per end user and 3 concurrent runs per link, and a maximum message length of 8,000 chars.

#### Scenario: Join table records ownership
- **WHEN** an end user starts a session on a share link
- **THEN** a `kb.agent_share_sessions` row is created linking the share link, the ACP session, and the end user's ref, with no ownership column added to `kb.acp_sessions`

#### Scenario: End user sees only their own sessions
- **WHEN** an end user requests their session list
- **THEN** only sessions whose join-table rows belong to their ref (and link) are returned

#### Scenario: Share sessions are hidden from normal listings
- **WHEN** a project member lists the project's ACP/chat sessions
- **THEN** ACP sessions linked from `kb.agent_share_sessions` are excluded, so anonymous public-share sessions never appear in a member's session list

#### Scenario: Archived filter
- **WHEN** an end user lists sessions with `filter=active|all|archived`
- **THEN** the list is filtered accordingly, defaulting to `active`

#### Scenario: Active-session cap enforced
- **WHEN** an end user attempts to open a 6th active session on the same link
- **THEN** the attempt is denied with 429 `share_session_limit`

#### Scenario: Concurrent-run cap enforced
- **WHEN** a link already has 3 concurrent runs and a 4th run is requested
- **THEN** the 4th run is rejected with 429 `share_busy`

#### Scenario: Message length cap enforced
- **WHEN** a message exceeds the link's `max_message_chars`
- **THEN** the stream is rejected before the agent runs

### Requirement: Session transcript is scoped to the verified ref
`GET /api/share/agent/sessions/:id` SHALL return the session belonging to the caller's verified ref, shaped as `{id, title, isArchived, messages: [{role, content}], createdAt}`. The transcript SHALL be derived from the ACP session's existing message history and SHALL include only user/assistant plain-text messages.

#### Scenario: Transcript returns only own session
- **WHEN** an end user fetches one of their own sessions by id
- **THEN** the response carries the id, title, archived flag, `createdAt`, and an ordered `messages` array of `{role, content}` pairs

#### Scenario: Foreign session is denied
- **WHEN** an end user fetches a session id not owned by their ref on this link
- **THEN** the request is denied and no transcript leaks

### Requirement: Email capture is metadata only
Email capture, required by default, SHALL be stored in `kb.agent_share_end_users` with a normalized copy. Email SHALL be treated as consent/contact metadata only and SHALL NEVER grant session access; access keys off `end_user_ref` alone.

#### Scenario: Unverified email grants no access
- **WHEN** an end user supplies an email that has not been verified
- **THEN** the email does not grant access to any session, even if it matches a stored record

#### Scenario: Email is captured on entry when required
- **WHEN** a link has `require_email` true and an end user starts a session without an email
- **THEN** the request is denied with 403 `share_email_required` until an email is provided

### Requirement: Anonymous tool approvals
Tool approvals SHALL be available to anonymous end users. The share-scoped respond endpoint SHALL verify question→run→`acp_session_id`→`kb.agent_share_sessions` ownership for the caller's `end_user_ref`, then drive the shared `RespondToQuestion` resume helper with an empty user id. A maximum of 10 approvals SHALL be permitted per session, and each decision SHALL record `kb.agent_tool_approvals.share_link_id` for audit correlation.

#### Scenario: Respond resumes the run anonymously
- **WHEN** an end user approves or denies a pending approval on a session they own
- **THEN** the run resumes via the shared respond/resume helper with no authenticated user id, and the decision is recorded with the link's share id

#### Scenario: Pending questions are delivered
- **WHEN** the shared agent raises an `ask_user` question or a tool approval
- **THEN** it is delivered for the bound end user's sessions and answerable through the share respond endpoint

#### Scenario: Ownership is verified before answering
- **WHEN** an end user attempts to answer a question whose run does not map to one of their sessions on the link
- **THEN** the request is denied, so a foreign ref can never answer another user's question

#### Scenario: Approvals disabled gate
- **WHEN** a link has `allow_end_user_approvals` false
- **THEN** end-user approvals are rejected with 403

#### Scenario: Approval cap enforced
- **WHEN** a session reaches 10 approvals and an 11th is requested
- **THEN** the 11th approval is denied with 429 `share_approval_limit`

### Requirement: Sandbox is config-gated and never receives owner credentials
Sandbox execution for a share link SHALL be config-gated and default OFF. When OFF, the run SHALL carry a nil sandbox config; in all cases the executor SHALL NOT mint an ephemeral org token (`DisableAuthMint`) and SHALL NOT inject the owner's project credentials (`AuthToken` empty).

#### Scenario: Sandbox defaults off
- **WHEN** a share link is created without an explicit sandbox setting
- **THEN** `sandbox_enabled` is false and the run's sandbox config is nil

#### Scenario: Anonymous run never mints owner credentials
- **WHEN** an anonymous end user runs a turn
- **THEN** the executor does not mint an ephemeral org token and does not inject the owner's project credentials

### Requirement: Suspended-run TTL reaper
Stale `input-required` share runs SHALL be auto-cancelled by a periodic reaper after the link's approval timeout (default 10 minutes) has lapsed, cancelling their pending questions and run consistently.

#### Scenario: Stale input-required run is cancelled
- **WHEN** a share run has been `input-required` beyond the link's `approval_timeout_seconds`
- **THEN** the reaper cancels its pending questions and the run, releasing its concurrency slot

#### Scenario: Fresh pending run is left alone
- **WHEN** a share run has been `input-required` for less than the approval timeout
- **THEN** the reaper leaves it pending for the end user to respond
