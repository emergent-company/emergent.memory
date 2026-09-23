## Purpose

Defines the scoped, revocable per-device credential that replaces the removed session-less `X-API-Key` / `MEMORY_TOKEN` path for the web gateway's voice/device clients (the iOS `VoiceAgent`). A device credential is an ordinary project-scoped `emt_*` API token in `core.api_tokens` carrying a reserved `device:api` marker scope, bounded by a hardcoded read-only ceiling and a surface guard enforced on both the server and the gateway, so a session-less device can reach only the voice/read surface and can never mint or escalate.

## ADDED Requirements

### Requirement: Device credential shape and mint path

A device credential SHALL be an `emt_*` project API token stored in `core.api_tokens` with `user_id = NULL`, carrying the reserved `device:api` marker scope plus the hardcoded scope set `device:api`, `agents:read`, `data:read` — strictly below `project_viewer`, with no `schema:read`, `projects:read`, write, admin, org, or other marker scope. It SHALL be minted only by the internal `CreateDeviceToken(projectID, name, expiresAt)` path, which hardcodes the scope set and accepts no caller-supplied scopes; the plaintext is returned exactly once at creation and stored server-side only as a hash.

#### Scenario: Mint produces the exact ceiling

- **WHEN** `CreateDeviceToken` is called for a project
- **THEN** the stored token's scope set is exactly `device:api`, `agents:read`, `data:read` and the response returns the `emt_*` plaintext once

#### Scenario: No caller can widen the scope set

- **WHEN** a caller attempts to mint a device credential with additional scopes
- **THEN** the additional scopes are ignored because `CreateDeviceToken` accepts no scopes parameter

#### Scenario: Non-user ownership

- **WHEN** a device credential is minted
- **THEN** the token row has `user_id = NULL` so it can never resolve as a user for user-scoped endpoints

### Requirement: The device marker is reserved to the internal mint path

The `device:api` marker SHALL be a member of the application's API-token vocabulary but SHALL NOT be attachable through any user-facing mint or update path. `Create`, `CreateAccountToken`, `UpdateScopes`, and `UpdateAccountTokenScopes` SHALL reject it, and the request DTO `oneof` tags SHALL omit it.

#### Scenario: User-facing create rejects the marker

- **WHEN** a user-facing token create/update request carries `device:api`
- **THEN** the request is rejected with a reserved-scope error before any repository access

#### Scenario: Only the internal path may mint it

- **WHEN** a token carrying `device:api` is minted
- **THEN** it is minted exclusively through `CreateDeviceToken`

### Requirement: Exact-set ceiling and surface guard fail closed

At validation time a token carrying the `device:api` marker whose scope set is not EXACTLY the ceiling SHALL be rejected. A `device:api` token used outside the device surface SHALL be rejected with 403. Unknown, revoked, expired, or store-unavailable credentials SHALL deny (never open); the ceiling and surface guard SHALL apply even to a DB-tampered device token.

#### Scenario: Out-of-ceiling scope set is rejected

- **WHEN** a `device:api` token's stored scopes are not exactly the ceiling (e.g. a write or admin scope was added)
- **THEN** the request is denied fail-closed before the token is trusted

#### Scenario: Out-of-surface use is rejected

- **WHEN** a `device:api` token is presented to a server endpoint outside the device surface (e.g. token mint, members, org, or write routes)
- **THEN** the request is denied with 403

#### Scenario: Unknown, revoked, expired, or store-down deny

- **WHEN** a device credential is unknown, revoked, expired, or the backing store is unavailable
- **THEN** the request is denied, never proxied or opened

### Requirement: Gateway device branch with a surface allowlist and token-derived context

The gateway SHALL recognise a device credential by introspecting the presented `emt_*` bearer for the reserved `device:api` marker, accept it only on the device-surface allowlist (room-token mint, agent picker, chat/session relay, memory browsing), proxy the credential verbatim as the Memory bearer, and derive project/org from the token introspection — never from raw `X-Project-ID` / `X-Org-ID` headers. A missing/non-`emt_*` bearer, a failed introspection, a non-device `emt_*` token, or an off-surface request SHALL fail closed with 401/403.

#### Scenario: Marker introspection recognises a device token

- **WHEN** a session-less request presents an `emt_*` bearer
- **THEN** the gateway introspects it (via memory's `/api/auth/me`) and treats it as a device credential only when it carries `device:api`

#### Scenario: Device surface is an allowlist

- **WHEN** a device credential is presented to a gateway route outside the device surface
- **THEN** the request is rejected with 403

#### Scenario: Project/org come from the token, not headers

- **WHEN** a device request carries `X-Project-ID` / `X-Org-ID` headers
- **THEN** the gateway ignores them and derives project/org from the token binding

#### Scenario: Non-device programmatic bearer is not device-class

- **WHEN** a session-less `emt_*` bearer does not carry `device:api`
- **THEN** the gateway rejects it with 401 rather than silently treating it as device-class

### Requirement: Per-device identity, expiry, revocation, and last-used

A device credential SHALL have per-device attribution (a distinct `core.api_tokens` row with its own name/prefix), a default 90-day expiry, and an individual `revoked_at`. The devices UI SHALL list and revoke the project's device credentials via the shared project-token surface. A device credential's `last_used_at` SHALL be touched on use in a best-effort, non-blocking manner (no synchronous write on the hot auth path).

#### Scenario: Revocation cuts access immediately

- **WHEN** an operator revokes a device credential
- **THEN** the credential is rejected on its next use and disappears from the devices list

#### Scenario: Last-used is touched without blocking auth

- **WHEN** a device credential authenticates
- **THEN** its `last_used_at` is updated asynchronously and a failed touch never blocks the request

### Requirement: Setup exchanges the one-time token for the device credential

`POST /api/setup` SHALL exchange the one-time setup token (minted on the session-authenticated devices page, which also mints the device credential server-side and stores it for a single claim) for the device credential, returning the `emt_*` value as `apiKey`. The retired 64-hex `ios_device_keys` registry SHALL authenticate nothing; the optional device manifest SHALL be accepted but no longer persisted.

#### Scenario: Setup returns the device credential

- **WHEN** a client exchanges a valid one-time setup token
- **THEN** the response `apiKey` is the `emt_*` device credential minted at QR time

#### Scenario: Setup fails closed without a bound credential

- **WHEN** a setup token has no bound device credential (e.g. minted without a session)
- **THEN** the exchange returns 401, never an empty bearer

#### Scenario: Registry key authenticates nothing

- **WHEN** a client presents a retired 64-hex registry key
- **THEN** the gateway rejects it; only the session or a `device:api` credential authenticates
