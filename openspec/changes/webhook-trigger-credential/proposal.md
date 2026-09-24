## Why

The GitHub webhook's gateway→memory forwarder bearer is a single static `AGENT_TRIGGER_TOKEN` with no reserved marker, no per-integration identity, no expiry, and no scope ceiling (#847, documented as an accepted residual risk in `09-security.md`). The real ingress gate is the HMAC signature, but the forwarder credential itself does not get the scoped-credential treatment the rest of the system now uses for machine credentials (device credential #857, `share:agent-chat`, `mcp:agent-call`).

## What Changes

- **Reserved marker.** Add `webhook:trigger` to the app-owned API-token vocabulary and to `pkg/auth`'s memory scope vocabulary. It is a pure marker (no `ScopeImplies` entry) so it implies nothing.
- **Internal mint path.** `Service.CreateWebhookTriggerToken(projectID, name, expiresAt)` mints a project-scoped `emt_*` credential with `user_id = NULL` and a hardcoded scope set — no caller-supplied scopes — so no caller can widen it. The marker is rejected by every user-facing create/update path (`rejectReservedScopes`).
- **Mint surface.** `POST /api/projects/:projectId/webhook-trigger-tokens` (handler `CreateWebhookTriggerToken`) exposes the mint path to operators, mirroring the device-token route's `RequireAuth()` gate exactly, so a credential can be created without writing code.
- **Exact-set ceiling + surface guard.** The stored ceiling is `webhook:trigger + agents:read + agents:write + data:read`. `validateAPIToken` rejects any `webhook:trigger` token whose scope set is not exactly that set (fail-closed, even for a DB-tampered row), and `rejectWebhookTokenOutsideSurface` confines the credential to the trigger route (`POST /api/projects/:projectId/agents/:id/trigger`) plus its `search-knowledge`→`/query` loopback.
- **Per-integration identity, expiry, rotation, revocation, attribution.** Each integration is its own named `core.api_tokens` row (own `last_used_at`, default expiry, `revoked_at`); rotation reuses the shared project-token `Regenerate`, revocation the shared `Revoke`.
- **Gateway.** The gateway keeps forwarding `AGENT_TRIGGER_TOKEN` as the `Authorization: Bearer`; the HMAC ingress gate and the fail-closed 503 + startup validation are unchanged. The credential is now a scoped per-integration credential, not a shared static secret.
- **Docs.** `09-security.md` removes the residual-risk wording; `10-api-contracts.md`, `02-memory-backend.md`, `08-deployment.md`, `04-go-application.md`, and `MEMORY_GUIDE.md` are updated.

## Decisions

1. **Reuse the device-credential machinery, do not invent a parallel mechanism.** The device credential (#857) already established the marker + hardcoded-ceiling + exact-set validate-time guard + surface-guard shape in `pkg/auth` and `domain/apitoken`; this change mirrors it.
2. **The ceiling keeps `agents:write`.** The trigger route (`RequireAPITokenScopes("agents:write")`) is a hard gate in `domain/agents` that is out of this change's ownership, so the credential must carry `agents:write` to trigger at all. The surface guard, not the ceiling, is what keeps that write off every non-trigger surface. This is stated honestly in the spec (effective-scope).
3. **Surface = trigger + query loopback.** The forwarded credential drives the review agent's own in-process `search-knowledge` loopback (`POST /api/projects/:projectId/query`), so that read-only route is part of the surface; everything else (mint, members, org, chat, skills, graph-write, search) is out.
4. **`agents:read` + `data:read` supply the loopback reads.** `agents:read` expands to `chat:use` (the `/query` gate), `data:read` to `search`/`graph:read`/`schema:read`/`journal:read` (the review agent's read surface).
5. **The mint surface mirrors the device-token gate.** `RequireAuth()` alone, matching `POST /api/projects/:projectId/device-tokens`: any authenticated principal may mint a scoped, ceiling-bound, expiring, revocable credential for the named project. The credential's authority is bounded by the exact-set ceiling and surface guard, so it can never exceed the trigger route + query loopback regardless of who minted it.

## Not in scope (stated plainly)

- **Simultaneous per-integration forwarding.** The gateway holds **one** forwarder credential (`AGENT_TRIGGER_TOKEN`) today. Server-side, each integration is its own named, expiring, revocable `core.api_tokens` row — but the gateway does not select a per-delivery credential. True simultaneous multi-integration forwarding is a follow-up candidate.

## Capabilities

### New Capabilities

- `webhook-trigger-credential`: the scoped, per-integration gateway→memory webhook trigger credential — marker, ceiling, surface guard, identity, expiry, rotation, revocation, and the effective-scope statement.

### Modified Capabilities

None. `web-device-credential` is the device analogue and is left in place; `api-token-audit` already generalizes the `last_used_at` touch to every `emt_*` token (including this credential).

## Impact

- `apps/server/domain/apitoken/`: `webhookTriggerScope`, `webhookTriggerScopes`, `CreateWebhookTriggerToken`, `rejectReservedScopes` extended; `ValidApiTokenScopes` gains `webhook:trigger`.
- `apps/server/pkg/auth/`: `webhookTriggerScope`/`webhookTriggerScopes`, `webhookScopesMatchCeiling`, `rejectWebhookTokenOutsideSurface`, wired into `validateAPIToken` (ceiling) and `RequireAuth` (surface guard); `memoryScopeVocabulary` gains `webhook:trigger`.
- `apps/web-ui/gateway/`: comment-only updates to `config.go`, `webhook_github.go`, `supervisor.go` (no functional change).
- `apps/web-ui/docs/`: `09-security.md`, `10-api-contracts.md`, `02-memory-backend.md`, `08-deployment.md`, `04-go-application.md`, `MEMORY_GUIDE.md`.
- Tests: unit (ceiling, surface, effective-scope) + DB-backed (mint/use/revoke/expire, per-integration isolation, tampered ceiling, fail-closed absent/store-down).

## Back-compat

`AGENT_TRIGGER_TOKEN` remains the gateway env var; operators replace its value with a minted `webhook:trigger` credential. The HMAC ingress gate and the fail-closed startup/503 behaviour are unchanged. Existing broad static tokens are NOT retroactively constrained (no migration marks them); operators migrate by re-minting.
