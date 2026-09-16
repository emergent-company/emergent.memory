## Context

See proposal.md — Why. The current MCP server authenticates a request into an `auth.AuthUser` carrying `Scopes`, `ProjectID`, and — when the credential is an API token — `APITokenID` (`pkg/auth/middleware.go:44-48`). The three transports converge on shared filtering: `handleToolsList` calls `GetToolDefinitionsForProject` then `FilterToolsForScopes` in `handler.go:256`, `streamable_http_handler.go:527`, and `sse_handler.go:305`; `tools/call` authorization currently relies on scope checks plus `AgentOnly` gating in `ExecuteTool`. Scope filtering lives in `service.go:1555` and is purely scope-driven — there is no per-token tool allowlist.

The legacy share flow (`mcp/share.go`) mints a normal project API token via `apitoken.Service.Create` with a fixed read-only scope set and stores nothing that identifies it as a share beyond the token name. `core.api_tokens` (`domain/apitoken/entity.go`) has no metadata column.

## Goals / Non-Goals

**Goals:**
- One durable, indexed lookup from an authenticated MCP request to its share instance, using data already on `AuthUser`.
- A single allowlist-filtering seam shared by all three transports, so listing and calling can never diverge.
- Reuse `apitoken` for credentials and the existing required-scope metadata for deriving token scopes.
- Legacy tokens keep working without migration or backfill.

**Non-Goals:**
- Changing the MCP JSON-RPC wire contract or supported protocol versions.
- Per-resource or per-object row-level scoping beyond tool/agent allowlists.
- OAuth / PKCE for MCP clients (the backend already advertises bearer-token auth).
- Sharing write access with granular per-tool read/write toggles; scopes are still derived from the tools chosen.

## Decisions

### 1. New `core.mcp_share_instances` table bound to `core.api_tokens.id`

**Decision:** Add an additive migration creating `core.mcp_share_instances` with `id`, `project_id`, `name`, `description`, `token_id` (FK), `allowed_tools text[]` (nullable), `allowed_agents uuid[]` (nullable), `is_legacy bool`, `created_by`, `created_at`, `updated_at`, `revoked_at`; unique index on `(project_id, lower(name))` where `revoked_at is null`.

**Rationale:** Instances need list/CRUD, name uniqueness, and a stable join target. The request path can resolve an instance by `AuthUser.APITokenID` with one indexed lookup — no token hashing or decrypt on the hot path.

**Alternatives considered:**
- *JSONB metadata column on `core.api_tokens`*: avoids a table but couples MCP concerns into the token domain and makes list/lifecycle queries awkward; rejected.
- *Instance as source of truth with token stored inline*: duplicates credential handling and loses the existing token list/revoke UI; rejected.

### 2. Reuse `apitoken.Service` for credential lifecycle

**Decision:** Create tokens with `apitoken.Service.Create(ctx, projectID, userID, name, scopes)`; update scopes with the existing scope-update path; revoke with `Service.Revoke`; rotate by creating a replacement token and revoking the old one.

**Rationale:** Tokens remain visible and revocable in the existing token UI, and `AuthUser.APITokenID` is populated automatically. Scopes are the **union of `RequiredScope` for the selected tools** plus `projects:read`, so the scope check stays consistent with the allowlist filter.

**Alternatives considered:** A dedicated `mcp_share_tokens` table (more auth surface, duplicated revocation); rejected.

### 3. One allowlist filter applied in the service layer, invoked by all transports

**Decision:** Introduce instance resolution + filtering in the `mcp` domain service (e.g. an `InstanceScope` value with `AllowedTools`/`AllowedAgents`), and call it from each transport's `tools/list`/`tools/call` path. Filtering order: `GetToolDefinitionsForProject` → `FilterToolsForScopes` → `FilterToolsForInstance`. `tools/call` re-checks the allowlist after scope authorization and before execution.

**Rationale:** Three transports already duplicate the scope-filter call; adding instance filtering to each risks drift. Keeping the predicate in the service makes it unit-testable once and enforceable everywhere.

**Alternatives considered:** Filtering only in `tools/list` (would let a caller invoke hidden tools directly — rejected as a security hole); middleware-only (Echo middleware lacks the per-method JSON-RPC context).
- **Enforcement is deny-by-default:** a call is allowed only if the tool is in the allowlist (or allowlist is null) **and** scope-permitted, so hidden tools can never be called.

### 4. Resolve the instance per request, not from the in-memory MCP session

**Decision:** On each `tools/list`/`tools/call`, resolve the instance from `AuthUser.APITokenID` (indexed) rather than caching allowlist state in the transport `Session`. Legacy tokens with no instance row yield a null allowlist.

**Rationale:** Sessions persist across requests (`handler.go`'s `sessions` map, `streamable_http_handler`'s `sessions`). Caching allowlists would make an admin's allowlist edit invisible until reconnect or would require cache invalidation across transports.

**Alternatives considered:** Caching with invalidation on update — more moving parts for negligible latency gain.

### 5. Agent scoping enforced where agent tools execute

**Decision:** Apply `allowed_agents` (when non-null) to agent-discovery results and to agent-execution tools in the service layer. Agent IDs are validated against the project on write and de-duplicated; a later-deleted agent simply resolves to nothing.

**Rationale:** Agent tools are project-enriched and execute through service paths, so the same seam governs listing and invocation.

### 6. Catalog endpoint returns includable tools

**Decision:** `GET /api/projects/:projectId/mcp/tools` returns the union of static + project tools with `name`, `description`, `requiredScope`, and `category`, excluding `AgentOnly`, ordered by category then name, admin-only.

**Rationale:** Mirrors exactly what a full-scope instance could be granted, so UI selection and validation use the same set the server enforces.

### 7. Security hardening: the allowlist is an execution boundary

**Decision:** Enforce the tool allowlist in three layers so it cannot be bypassed by triggering an agent:

1. **Inside `Service.ExecuteTool`**, using `InstanceScopeFromContext(ctx)`, so every caller — including the ADK `ToolPool`, which executes tools with the agent's context rather than the HTTP request context — is covered.
2. **Reject at create/update** any tool allowlist that includes an agent-execution (`trigger_agent`, `acp-trigger-run`) or agent-mutation tool, because their side effects run outside the request context and may be queued/async.
3. **Exclude** agent-execution and agent-mutation tools from the tool catalog entirely, so they can never be selected.

For synchronous agent runs the scoped context is threaded through `delegateAgentTool` into the agent handler; for asynchronous runs the allowlist cannot be relied upon, which is why (2)/(3) fail closed at write time.

Agent gating also fails closed: agent references are resolved by **name or ACP slug** (shared `pkg/acpslug`, re-exported by the agents domain), and an unresolvable/foreign reference, a directory error, or a malformed discovery payload under an active agent allowlist denies the call / returns an empty result. Run-inspection and agent-mutation tools are hidden and rejected whenever an agent allowlist is active because they cannot be reliably mapped to runtime agent IDs; agent-definition reads are filtered to the allowed agent names/slugs (unmatched reads return not-found). `allowed_agents` is stored as `uuid[]` (`[]uuid.UUID`, repo convention) and rotate is atomic: the token revoke+insert and the instance `token_id` update run in one transaction.

**Rationale:** A transport-only filter is insufficient once an agent can execute tools on the instance's behalf; the boundary must live where tools actually execute.

## Risks / Trade-offs

- [Scope union may omit a scope a tool needs at call time] → Derive scopes from the same `ToolDefinition.RequiredScope` the filter uses; add a unit test asserting every allowlisted tool passes the call-time scope+allowlist check.
- [Existing sessions carry stale scopes after a token scope update] → Resolve allowlist/scopes per request; accept that transport sessions store scopes only for display, and re-check against current token state on `tools/call`.
- [Allowlist edits while a client is connected] → Because filtering is per request, edits take effect on the next request without reconnect; document this in the UI as immediate.
- [Legacy tokens regressing] → `is_legacy`/absent instance row yields null allowlist; covered by explicit specs and tests.
- [Agent IDs become dangling after deletion] → Resolve to no agent rather than error; covered by a spec scenario.

## Migration Plan

1. Add the additive migration (new table + indexes); no backfill.
2. Ship the service filtering, routes, and catalog behind the new endpoints.
3. Legacy tokens are treated as legacy instances lazily; optionally surface them in the list.
4. **Rollback:** drop the new table and revert handlers. Tokens minted by instances remain valid API tokens (they simply lose allowlist scoping), matching legacy behavior.

## Open Questions

- Should share-instance tokens default to a finite expiry (e.g. 90 days)? The schema supports `expires_at`; defaulting can be decided at implementation without changing the specs.
