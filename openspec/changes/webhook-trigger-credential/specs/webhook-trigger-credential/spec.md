## Purpose

Defines the scoped, revocable per-integration webhook trigger credential that replaces the shared static `AGENT_TRIGGER_TOKEN` for the gateway→memory PR-review trigger. A webhook trigger credential is an ordinary project-scoped `emt_*` API token in `core.api_tokens` carrying a reserved `webhook:trigger` marker scope, bounded by a hardcoded ceiling and a surface guard enforced server-side, so the session-less GitHub webhook can only trigger the review agent (plus its own `search-knowledge`→`/query` loopback) and can never reach a wider surface.

## ADDED Requirements

### Requirement: Webhook trigger credential shape and mint path

A webhook trigger credential SHALL be an `emt_*` project API token stored in `core.api_tokens` with `user_id = NULL`, carrying the reserved `webhook:trigger` marker scope plus the hardcoded **stored** scope set `webhook:trigger`, `agents:read`, `agents:write`, `data:read`. The `agents:write` member SHALL be present solely to satisfy the trigger route's own `agents:write` gate; by scope expansion (`ScopeImplies`) `agents:write` SHALL expand to `chat:admin`/`skills:write`, `agents:read` to `chat:use`/`skills:read`, and `data:read` to the read family (including `schema:read`, `search`, `graph:read`, `journal:read`), so the **effective** scope set SHALL include those implied scopes and the spec SHALL NOT claim they are absent. It SHALL be minted only by the internal `CreateWebhookTriggerToken(projectID, name, expiresAt)` path, which hardcodes the stored scope set and accepts no caller-supplied scopes; the plaintext SHALL be returned exactly once at creation and stored server-side only as a hash.

#### Scenario: Mint produces the exact ceiling

- **WHEN** `CreateWebhookTriggerToken` is called for a project
- **THEN** the stored token's scope set is exactly `webhook:trigger`, `agents:read`, `agents:write`, `data:read` and the response returns the `emt_*` plaintext once

#### Scenario: No caller can widen the scope set

- **WHEN** a caller attempts to mint a webhook credential with additional scopes
- **THEN** the additional scopes are ignored because `CreateWebhookTriggerToken` accepts no scopes parameter

#### Scenario: Non-user ownership

- **WHEN** a webhook trigger credential is minted
- **THEN** the token row has `user_id = NULL` so it can never resolve as a user for user-scoped endpoints

### Requirement: The webhook marker is reserved to the internal mint path

The `webhook:trigger` marker SHALL be a member of the application's API-token vocabulary and of `pkg/auth`'s memory scope vocabulary, but SHALL NOT be attachable through any user-facing mint or update path. `Create`, `CreateAccountToken`, `UpdateScopes`, and `UpdateAccountTokenScopes` SHALL reject it, and the request DTO `oneof` tags SHALL omit it.

#### Scenario: User-facing create rejects the marker

- **WHEN** a user-facing token create/update request carries `webhook:trigger`
- **THEN** the request is rejected with a reserved-scope error before any repository access

#### Scenario: Only the internal path may mint it

- **WHEN** a token carrying `webhook:trigger` is minted
- **THEN** it is minted exclusively through `CreateWebhookTriggerToken`

### Requirement: Webhook trigger credential mint surface

An operator SHALL be able to mint a webhook trigger credential without writing code, through an HTTP mint surface that mirrors the device-token analogue: `POST /api/projects/:projectId/webhook-trigger-tokens`, handled by `Handler.CreateWebhookTriggerToken`, which calls `CreateWebhookTriggerToken(projectID, name, expiresAt)` with the hardcoded ceiling and a default 90-day expiry. The route SHALL authorize the caller against the addressed project via the canonical middleware pair — `RequireAuth`, then `RequireProjectTokenScope`, then `RequireProjectMember` — so an unauthenticated caller is denied with 401, a session caller who is not a member of the project's owning organization is denied with 403, and a project-bound `emt_*` token minted for a different project is denied with 403. A member (or a token bound to the addressed project) may mint; the credential's authority remains bounded by the exact-set ceiling and the surface guard, so it can never exceed the trigger route plus its query loopback regardless of who minted it.

#### Scenario: A member mints through the HTTP surface

- **WHEN** a member of the addressed project's owning organization POSTs to `/api/projects/:projectId/webhook-trigger-tokens` with a name
- **THEN** a scoped webhook trigger credential is minted (exact ceiling, the operator name, a default expiry) and the raw `emt_*` value is returned once

#### Scenario: A non-member is refused

- **WHEN** an authenticated session caller who is not a member of the addressed project's owning organization POSTs to the webhook-trigger-tokens route
- **THEN** the request is denied with 403

#### Scenario: A cross-project token is refused

- **WHEN** a project-bound `emt_*` token minted for a different project POSTs to the webhook-trigger-tokens route
- **THEN** the request is denied with 403

#### Scenario: Unauthenticated mint is refused

- **WHEN** an unauthenticated request POSTs to the webhook-trigger-tokens route
- **THEN** the request is denied with 401, mirroring the device-token route's `RequireAuth` gate

### Requirement: Exact-set ceiling and surface guard fail closed

At validation time a token carrying the `webhook:trigger` marker whose scope set is not EXACTLY the ceiling SHALL be rejected. A `webhook:trigger` token used outside the webhook trigger surface (the trigger route plus its `search-knowledge`→`/query` loopback) SHALL be rejected with 403. Unknown, revoked, expired, or store-unavailable credentials SHALL deny (never open); the ceiling and surface guard SHALL apply even to a DB-tampered webhook token.

#### Scenario: Out-of-ceiling scope set is rejected

- **WHEN** a `webhook:trigger` token's stored scopes are not exactly the ceiling (e.g. a write or admin scope was added)
- **THEN** the request is denied fail-closed before the token is trusted

#### Scenario: Out-of-surface use is rejected

- **WHEN** a `webhook:trigger` token is presented to a server endpoint outside the webhook trigger surface (e.g. token mint, members, org, chat, skills, or graph-write routes)
- **THEN** the request is denied with 403

#### Scenario: Unknown, revoked, expired, or store-down deny

- **WHEN** a webhook credential is unknown, revoked, expired, or the backing store is unavailable
- **THEN** the request is denied, never proxied or opened

### Requirement: Per-integration identity, expiry, revocation, and attribution

A webhook trigger credential SHALL have per-integration attribution (a distinct `core.api_tokens` row with its own name and prefix), a default expiry, and an individual `revoked_at`. Rotation SHALL reuse the shared project-token `Regenerate` path, which atomically revokes the old credential and mints a replacement with the same name and ceiling; revocation SHALL reuse the shared `Revoke` path. A webhook credential's `last_used_at` SHALL be touched on use in a best-effort, non-blocking manner, consistent with the `api-token-audit` capability.

#### Scenario: Revocation cuts access immediately

- **WHEN** an operator revokes a webhook credential
- **THEN** the credential is rejected on its next use and disappears from the project's token list

#### Scenario: Rotating one integration does not affect another

- **WHEN** integration X's credential is revoked or rotated
- **THEN** integration Y's credential remains valid, because each integration is its own token row

#### Scenario: Last-used is touched without blocking auth

- **WHEN** a webhook credential authenticates
- **THEN** its `last_used_at` is updated asynchronously and a failed touch never blocks the request

### Requirement: Gateway forwards the scoped credential with the HMAC gate unchanged

The gateway SHALL forward `AGENT_TRIGGER_TOKEN` — now a `webhook:trigger`-marked scoped credential — as the `Authorization: Bearer` on the trigger call, and SHALL NOT weaken or bypass the HMAC ingress gate (`verifyGitHubSignature`, constant-time `hmac.Equal` on `X-Hub-Signature-256`). A missing or empty credential SHALL fail closed: `Config.Validate` SHALL refuse startup when `GITHUB_WEBHOOK_SECRET` is set but `AGENT_TRIGGER_TOKEN` is empty, and the handler SHALL return 503 before spawning the async trigger when the token is missing. The gateway holds **one** forwarder credential today; simultaneous per-integration forwarding (multiple integrations each with their own gateway-held credential, selected per delivery) is out of scope for this change and SHALL NOT be claimed as achieved.

#### Scenario: HMAC remains the ingress gate

- **WHEN** a webhook delivery is presented
- **THEN** the `X-Hub-Signature-256` header is verified in constant time and an invalid signature is rejected with 401 regardless of the forwarded credential

#### Scenario: Missing credential fails closed

- **WHEN** the webhook secret is configured but `AGENT_TRIGGER_TOKEN` is empty
- **THEN** startup validation fails and the handler returns 503 before the async trigger is spawned

#### Scenario: Operational install

- **WHEN** an operator mints a `webhook:trigger` credential through the mint surface and installs its `emt_*` value as `AGENT_TRIGGER_TOKEN`
- **THEN** the credential validates as marker-class and ceiling-bound, expires on schedule, and is revocable and regenerable through the shared project-token surface; `last_used_at` attributes its use
