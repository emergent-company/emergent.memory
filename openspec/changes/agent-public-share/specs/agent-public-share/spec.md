## Purpose

Defines how an owner mints a public, keyed, shareable link to a single agent definition — the reserved-scope credential and its binding, the authorization that requires both, owner management (including on-demand key reveal), and the per-link budget/expiry and tool deny/allowlist — while keeping the server free of unauthenticated routes.

## ADDED Requirements

### Requirement: A share link mints a reserved-scope credential with a binding
A share link SHALL reuse `core.api_tokens` with a new reserved marker scope `share:agent-chat` minted only by the internal `CreateAgentChatShareToken` path (a clone of the `mcp:agent-call` mint pattern). The minted token SHALL carry **only** `share:agent-chat` — no `projects:read` — and SHALL have `user_id = NULL` (project_id still set). The link SHALL bind the credential to one agent definition via `kb.agent_share_links`, whose `api_token_id` column is UNIQUE. User-facing token create/update paths SHALL reject the reserved scope with a "reserved" error.

#### Scenario: User-facing create rejects the reserved scope
- **WHEN** a user attempts to create or update a token carrying the `share:agent-chat` scope through the normal token API
- **THEN** the request is rejected with a "reserved" error and no token is minted

#### Scenario: Only the internal share path sets the marker
- **WHEN** a share link is minted
- **THEN** the `share:agent-chat` marker is set only by `CreateAgentChatShareToken`, which mints `user_id = NULL` and no scope other than `share:agent-chat` (no `projects:read`)

#### Scenario: One credential binds to exactly one link
- **WHEN** the same `api_token_id` is bound to a second share link
- **THEN** the second bind is rejected by the UNIQUE constraint, so one credential can never authorize two links

#### Scenario: Each link targets one agent definition in one project
- **WHEN** a share link is created
- **THEN** it binds exactly one `agent_definition_id` and one `project_id`; the link cannot be reassigned to a different definition

### Requirement: Authorization requires scope AND binding
Authorization for a share-scoped request SHALL require BOTH the `share:agent-chat` marker scope AND successful token→link binding resolution. A credential carrying the marker scope with no bound link SHALL be denied (401); a bound link whose token lacks the marker scope SHALL be denied; a revoked or expired link SHALL be denied with 410.

#### Scenario: Scope without binding is denied
- **WHEN** a token carries `share:agent-chat` but resolves to no `kb.agent_share_links` row
- **THEN** the request is denied with an unauthorized (401) error

#### Scenario: Binding without scope is denied
- **WHEN** a token resolves to a bound share link but does not carry `share:agent-chat`
- **THEN** the request is denied with a forbidden (403) error

#### Scenario: Revoked or expired link is denied with 410
- **WHEN** a token resolves to a share link that is revoked or past its expiry
- **THEN** the request is denied with HTTP 410 and a `share_link_revoked`/`share_link_expired` code

#### Scenario: Scope and binding together authorize
- **WHEN** a token carries `share:agent-chat` and resolves to a bound, non-revoked, non-expired share link
- **THEN** the request is authorized scoped to that link's agent definition and project

### Requirement: Share marker is unusable outside the share surface
Any request carrying the `share:agent-chat` marker SHALL be rejected with 403 on every route outside `/api/share/agent`. A leaked share key SHALL therefore be unable to reach project, member, or other authenticated endpoints.

#### Scenario: Marker rejected outside the share surface
- **WHEN** a request carries `share:agent-chat` to a path not under `/api/share/agent`
- **THEN** the auth middleware returns 403 and the handler never runs

#### Scenario: Marker allowed inside the share surface
- **WHEN** a request carries `share:agent-chat` to a path under `/api/share/agent`
- **THEN** the surface guard does not reject it, and the normal scope + binding resolution proceeds

### Requirement: Project and org resolve only from the bound link
Project and org identity for a share-scoped request SHALL be resolved exclusively from the bound share link. Any client-supplied project or org selector (for example `X-Project-ID` or `X-Org-ID` headers) SHALL be ignored and stripped.

#### Scenario: Client project header is ignored
- **WHEN** a share request includes an `X-Project-ID` header naming a different project
- **THEN** the request is scoped to the bound link's project, not the header's project

#### Scenario: Client org header is ignored
- **WHEN** a share request includes an `X-Org-ID` header
- **THEN** the org is resolved from the bound link and the header is ignored

### Requirement: Owner management API
An owner SHALL be able to create, list, get, update, revoke, and rotate a share link for an agent definition they administer, and read its usage. Create and rotate SHALL return the raw key exactly once. A separate on-demand reveal endpoint SHALL recover the key of an existing link.

#### Scenario: Create returns the key exactly once
- **WHEN** an owner creates a share link
- **THEN** the response contains the link id and the raw key exactly once; the key is not stored in plaintext afterward

#### Scenario: Reveal recovers the key on demand
- **WHEN** an owner requests `GET /api/projects/:projectId/share-links/:linkId/reveal` for a recoverable link
- **THEN** the response contains `{key}`, recovered from the encrypted token, and list rendering never returns the key

#### Scenario: Non-recoverable key returns 422
- **WHEN** an owner requests reveal for a link whose token cannot be decrypted
- **THEN** the response is HTTP 422 with code `not_recoverable` (never a partial or ambiguous value)

#### Scenario: Rotation swaps the credential and keeps the link
- **WHEN** an owner rotates a share link
- **THEN** a new reserved-scope credential is minted and bound to the same link, the previous credential is revoked, and the link id is preserved

#### Scenario: Revocation denies immediately
- **WHEN** an owner revokes a share link
- **THEN** subsequent requests using that link's credential are denied immediately

#### Scenario: Revocation cascades to the bound credential
- **WHEN** an owner revokes or deletes a share link
- **THEN** the bound `core.api_tokens` row is revoked immediately, not just the link row, so the share key stops working right away regardless of the link's own `expires_at`

#### Scenario: Non-admin is denied
- **WHEN** a user without admin rights over the agent's project attempts to create or revoke a share link
- **THEN** the request is denied with a forbidden error and nothing changes

### Requirement: Per-link budget, expiry, and retention config
Each share link SHALL carry a config with a rolling 24-hour budget of 500 messages, 200k tokens, and $5 of spend, a link expiry of 30 days, and a retention of 90 days after last activity. Spend SHALL be denied before it occurs, using rolling counters in `kb.agent_share_usage` keyed by (link, period).

#### Scenario: Message budget is reserved atomically
- **WHEN** concurrent turns arrive for one link in the same rolling period
- **THEN** each turn reserves its message slot by locking the `(link, period)` usage row with `SELECT … FOR UPDATE` and incrementing the message count inside the transaction, so concurrent turns cannot all pass a check-then-act gate

#### Scenario: Tokens and cost are settled after the run
- **WHEN** a reserved turn completes
- **THEN** its actual token and cost usage are added to the same rolling-window bucket; the message slot was already reserved up-front

#### Scenario: Budget exhaustion denies before spend
- **WHEN** a link's rolling budget (messages, tokens, or spend) is exhausted
- **THEN** further messages are denied with 429 `share_budget_exceeded` before any additional spend occurs (the reservation transaction rolls back and nothing is reserved)

#### Scenario: Rolling window resets
- **WHEN** the rolling 24h period advances
- **THEN** a new usage period begins and the link can spend again up to the budget

#### Scenario: Expired link is denied
- **WHEN** a link's expiry has passed
- **THEN** requests through that link are denied and the owner must rotate or re-mint

#### Scenario: Retention window is configurable
- **WHEN** a link is created or updated
- **THEN** its `retention_days` config governs how long end-user data is retained after last activity

### Requirement: Per-link tool deny/allowlist
A share link SHALL hard-deny dangerous tools by default. The built-in deny set (entity/schema/document mutation, provider configuration, token/credential minting, skill/blueprint/embedding/agent-hook/branch mutation, `forget`, and similar) SHALL be blocked unless an owner explicitly re-enables a tool via `tool_allowlist`. The deny set SHALL be enforced in the executor BEFORE the confirm gate, so an approval can never override a deny-listed tool.

#### Scenario: Dangerous tool is denied by default
- **WHEN** the shared agent calls a tool in the default dangerous set that is not allowlisted
- **THEN** the tool is blocked with a `share_deny` result before any approval prompt is raised

#### Scenario: Allowlist re-enables a dangerous tool
- **WHEN** an owner adds a dangerous tool to the link's `tool_allowlist`
- **THEN** the tool is removed from that link's deny set and is governed by the normal tool approval policy

#### Scenario: Deny outranks approval
- **WHEN** a tool is both deny-listed and under an `ask` approval policy
- **THEN** the deny is enforced before the confirm gate, so the approval prompt never renders and the tool never runs
