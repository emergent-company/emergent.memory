## Context

See proposal.md — Why. Agent-over-MCP sharing today is two unrelated features. Feature A is the project-scoped share instance `core.mcp_share_instances` (migration `apps/server/migrations/00144_add_mcp_share_instances.sql`; model `apps/server/domain/mcp/share_instance.go:154-174`) with `allowed_tools TEXT[]`, `allowed_agents UUID[]`, and a single `token_id`. Feature B is the per-agent share `core.agent_mcp_shares` (migration `apps/server/migrations/00148_create_agent_mcp_shares.sql`, model `apps/server/domain/mcp/agent_mcp_share.go:69-87`) with a single `token_id` and `UNIQUE(token_id) WHERE revoked_at IS NULL` (00148:28-30). "Choosing an agent" is ambiguous: in A it is only an allowlist filter on the project-wide tool server (`normalizeAgentAllowlist`, `share_instance.go:933`); in B it is a real binding. This change removes the ambiguity by deleting the agent picker from A and making B agent-owned.

The per-agent endpoint is `/api/mcp/agents/:agentId` (`apps/server/domain/mcp/routes.go:74-88`), handled by `agent_endpoint_handler.go`, which exposes EXACTLY ONE tool `call_agent` (`agent_endpoint_handler.go:35`) → `Service.CallAgentOnce` (`agent_mcp_share.go:634`) → `agents.MCPToolHandler.RunAgentOnce` (`apps/server/domain/agents/agent_run_once.go:50`), bounded by hardcoded constants `agentShareRunMaxSteps = 12` and `agentShareRunTimeout = 60s` (`agent_mcp_share.go:58-61`). Keys live in `core.api_tokens`; agent shares mint the reserved scope `mcp:agent-call` plus `projects:read` (`agent_mcp_share.go:27,44`); the marker is rejected on every project MCP transport and required on the agent endpoint. Agent-share UI handlers and templ already exist (`apps/web-ui/gateway/agent_mcp_shares*.go/.templ`) but their routes are NOT registered in the gateway `main.go`, so they are unreachable; project-share UI routes ARE registered (`main.go:119-125,309-311`). `kb.agents` hard-deletes (no soft-delete; an `enabled` bool only) and `agent_mcp_shares.agent_id` has no FK. No CLI command exists for either share type.

Conversation continuity already exists in the executor and is the crux of the design. `ExecuteRequest.SessionID` (`apps/server/domain/agents/executor.go:265-269`) keys the ADK session as `"session:" + projectID + ":" + SessionID` (`executor.go:1495-1499`); on a later run with the same key the executor loads, token-trims, and LLM-compresses prior events (`executor.go:2204-2333`). `call_agent` simply never sets `SessionID`, so each call is a fresh session keyed by the root run id. Messages persist in `kb.agent_run_messages` (`apps/server/domain/agents/entity.go:398-413`; write `CreateMessage repository.go:1718`, read `FindMessagesByRunID:1727`), with an ordered transcript per session via `GetConversationFullHistory` (`repository.go:3170`). Chat/trigger sessions share the ADK store with `user_id='system'` (`executor.go:1015-1017`), so MCP sessions share a namespace — acceptable because session refs are UUIDs.

**Stacking prerequisite:** this change stacks on the implemented-but-unarchived changes `add-agent-mcp-endpoint` and `add-mcp-share-instances`. Because `openspec/specs/` does not yet contain `agent-mcp-endpoint`, `agent-mcp-shares`, or `mcp-share-instances`, the MODIFIED deltas validate as valid but archive will refuse until those changes are archived first. Migration numbering MUST remain sequential after `00148` and `00150`.

## Goals / Non-Goals

**Goals:**
- An agent owns its MCP endpoint: one active endpoint per agent, created and revoked from the agent's own configuration surface.
- One endpoint many keys: each labeled, individually revocable and rotatable, with last-used tracking, replacing the single `token_id` binding.
- A fixed minimal session tool set with no per-endpoint tool picking, one-shot behavior preserved, and opt-in persistent sessions.
- Reuse the executor's existing `SessionID` continuity rather than building a new history store.
- Keep project share instances working as a tools-only feature, with all other behavior unchanged.

**Non-Goals:**
- Async/polling result delivery (v1 stays synchronous).
- Per-key rate limits or quotas.
- Per-endpoint tool picking.
- Sharing one session across keys.
- Pinning an agent-definition version per endpoint.
- Streaming progress.
- CLI commands for either share type (deferred).
- A `default_mode` column.

## Decisions

### 1. One agent-owned endpoint per agent in `core.agent_mcp_endpoints`

**Decision:** Persist the endpoint as a new table keyed by `agent_id`, with a partial unique index restricting one active endpoint per agent, and `ON DELETE CASCADE` to `kb.agents(id)`.

**Rationale:** `kb.agents` hard-deletes, so today's missing FK on `agent_mcp_shares.agent_id` can orphan bindings; `ON DELETE CASCADE` makes endpoint lifetime follow the agent. A dedicated table (rather than columns on `kb.agents`) keeps credentials out of the dense, heavily-ALTERed agent row and off the auth hot path, which must not load a whole agent row. Alternatives: put endpoint state on `kb.agents` (widens a hot, already-fat table) or keep the single-share table (cannot model many keys).

### 2. Many labeled keys per endpoint in `core.agent_mcp_keys`, with `token_id` moved to the key row

**Decision:** Each key row binds one `core.api_tokens` row (`token_id UUID NOT NULL UNIQUE`) to one endpoint, carries a `label`, and has `created_by`/timestamps/`revoked_at`.

**Rationale:** `token_id UNIQUE` preserves the old "one credential cannot authorize two agents" guarantee that was `uq_agent_mcp_shares_active_token` (00148:28-30), so a token still maps unambiguously to a single key and thereby a single agent. Moving the FK off the endpoint is what makes many keys possible. Do NOT denormalize `last_used_at`/`expires_at`: they already live on `core.api_tokens` and are maintained by the auth middleware (`pkg/auth/middleware.go`). `key.label` maps to `token.name = "Agent MCP Key: <label>"`, mirroring the existing `agentShareTokenName` truncation (`agent_mcp_share.go:314-321`) so a long label cannot overflow `varchar(255)`.

### 3. Agent-scoped handler keeps the JSON-RPC mechanics, replaces the catalog

**Decision:** Keep `agent_endpoint_handler.go` sharing the request/response/error types and `streamable_http_handler` session mechanics, but replace the single static tool definition with a fixed five-tool catalog and a dispatch table.

**Rationale:** Avoids duplicating JSON-RPC framing, keeps the marker-scope isolation intact, and avoids touching the project catalog's scope/instance filtering.

### 4. Mode is selected by tool choice, never a parameter

**Decision:** There is no `mode` argument and no `default_mode` column. `call_agent` is the one-shot path; `start_session`/`continue_session`/`get_session`/`list_sessions` are the persistent path.

**Rationale:** A param or stored default would double the state space and break the "fixed minimal tool set" contract. Tool choice is self-documenting to an external LLM and keeps `call_agent` byte-identical.

### 5. Session ownership in `core.agent_mcp_sessions`, history in the ADK session store

**Decision:** Persist only ownership/metadata (endpoint, key, `session_ref`, status, counters, timestamps) in `core.agent_mcp_sessions`. Message history remains in the ADK session store under `session:<projectID>:<sessionRef>`, reached by passing `ExecuteRequest.SessionID = session_ref`. Sessions are key-scoped (`key_id`), not endpoint-scoped. The continue path issues a fresh `Execute` with the stored `SessionID`; it MUST NOT use `Resume`.

**Rationale:** The executor already does cross-run load/token-trim/LLM-compress when `SessionID` matches, so the continue path is a new session-aware run entry point that sets `SessionID`, not a new history store. `Resume` (`executor.go:781-786`) only resumes a PAUSED run for tool-response injection and cannot continue a completed run. Reusing the existing store avoids duplicating history and keeps trimming/compression in one place.

**Rejected stores:** `kb.chat_conversations`/`chat_messages` are chat-UI-shaped (`title`, `owner_user_id`, `is_private`, `enabled_tools`) and MUST NOT be reused as the session store. `kb.acp_sessions` (`entity.go:550-563`) is only a `(project_id, agent_name)` run-grouping entity with no history and MUST NOT be used as the conversation store. The `adk-session-list`/`adk-session-get` tools only match `kb.adk_sessions.id = kb.agent_runs.id::text` (`repository.go:2197,2216`), so they will NOT see custom session keys — a known dead end; session inspection goes through `get_session`/`list_sessions` instead.

### 6. Target schema

```sql
CREATE TABLE core.agent_mcp_endpoints (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id  UUID NOT NULL,
  agent_id    UUID NOT NULL REFERENCES kb.agents(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_agent_mcp_endpoints_agent
  ON core.agent_mcp_endpoints (agent_id) WHERE revoked_at IS NULL;

CREATE TABLE core.agent_mcp_keys (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  endpoint_id UUID NOT NULL REFERENCES core.agent_mcp_endpoints(id) ON DELETE CASCADE,
  token_id    UUID NOT NULL UNIQUE REFERENCES core.api_tokens(id) ON DELETE CASCADE,
  label       TEXT NOT NULL,
  created_by  UUID,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at  TIMESTAMPTZ
);
CREATE INDEX idx_agent_mcp_keys_endpoint ON core.agent_mcp_keys (endpoint_id);
CREATE UNIQUE INDEX uq_agent_mcp_keys_endpoint_label
  ON core.agent_mcp_keys (endpoint_id, lower(label)) WHERE revoked_at IS NULL;

CREATE TABLE core.agent_mcp_sessions (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  endpoint_id    UUID NOT NULL REFERENCES core.agent_mcp_endpoints(id) ON DELETE CASCADE,
  key_id         UUID NOT NULL REFERENCES core.agent_mcp_keys(id) ON DELETE CASCADE,
  session_ref    TEXT NOT NULL,
  status         TEXT NOT NULL DEFAULT 'active',
  turn_count     INT  NOT NULL DEFAULT 0,
  total_steps    INT  NOT NULL DEFAULT 0,
  last_run_id    UUID,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_active_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_agent_mcp_sessions_ref ON core.agent_mcp_sessions (session_ref);
CREATE INDEX idx_agent_mcp_sessions_key ON core.agent_mcp_sessions (key_id);
```

**Rationale:** `core` is the infra/credential schema — where `api_tokens`, `mcp_share_instances`, and `agent_mcp_shares` already live — while `kb` is knowledge. `kb.agents` is dense and already heavily ALTERed, and auth is a hot path that must not load a whole agent row. `ON DELETE CASCADE` on the hard-deleted `kb.agents` replaces today's missing FK. `token_id` moves to the key row so `token_id UNIQUE` preserves the old one-credential-one-agent guarantee. `last_used_at`/`expires_at` are deliberately NOT denormalized onto keys or sessions: they already live on `core.api_tokens` and are maintained by the auth middleware.

### 7. Tool contract

| Tool | Params (required) | Result |
|---|---|---|
| `call_agent` | `{message}` (required) | bare text reply, UNCHANGED for back-compat (`agent_mcp_share.go:643`; `agentRunErrorResult` 648-668) |
| `start_session` | `{message}` (optional) | envelope `{session_id, reply?, run_id, status}` |
| `continue_session` | `{session_id, message}` (both required) | envelope `{session_id, reply, run_id, status}` |
| `get_session` | `{session_id}` (required) | envelope `{session_id, status, created_at, last_active_at, turn_count}` |
| `list_sessions` | none | envelope `{sessions:[{session_id,status,created_at,last_active_at,turn_count}]}` |

**Decision:** The new session tools MUST return results through `envelopeResult` (`apps/server/domain/mcp/envelope.go:25-45`), per the archived `mcp-result-envelope-contract` change; `call_agent` MUST NOT be re-enveloped, because that is a breaking change for existing clients. Success is `envelopeResult(true, data, meta, "")`; failure is `envelopeResult(false, nil, meta, errMsg)` with `meta {steps, run_id}`. `AgentRunError` kinds (`agents/entity.go:165-185`: `agent_unavailable`, `run_failed`, `input_required`, `budget_exceeded`) map to `ok:false` plus `error` plus `meta.kind`. `start_session` with `message` present runs turn 1 and returns the reply; absent, it creates an empty session and returns only `session_id`.

### 8. Authorization: `AuthorizeAgentEndpoint(ctx, apiTokenID, agentID) -> (endpoint, key, error)`

**Decision:** Replace `AuthorizeAgentShare` (`agent_mcp_share.go:613-629`) with `AuthorizeAgentEndpoint`, which resolves the active key by `token_id`, then the active endpoint by `key.endpoint_id`, rejects `endpoint.agent_id != agentID` with 403, rejects a revoked/expired token with 403, and additionally resolves the agent and rejects `!agent.Enabled` with 403 before any run starts.

**Rationale:** Identity at runtime is PER-KEY (`key.id` scopes sessions, `last_used_at` is per token) while the authorization target is PER-ENDPOINT (the agent). The `mcp:agent-call` marker scope stays as-is. Adding the `agent.Enabled` check fast-fails at the endpoint instead of only at run time. Rotation is `RegenerateWith` on the key's `token_id` keeping the `key_id`, so sessions survive rotation; revocation revokes the token and sets `key.revoked_at`.

### 9. Session ids are not capabilities

**Decision:** Every session tool re-authorizes the key and verifies `row.key_id == authorizedKey.id`; any other key's session ref resolves as 404/403.

**Rationale:** Session refs travel through an LLM and may leak into logs; they must never be a bearer capability. Key scoping also means revoking one key does not strand another key's sessions.

### 10. Concurrency control per session (top risk)

**Decision:** Serialize `continue_session` on one session with `pg_advisory_xact_lock(hashtextextended(session_ref, 0))` around dispatch, or an optimistic CAS on `status='running'`, returning `ok:false, error:"session busy"`.

**Rationale:** Two concurrent continues mutate one ADK session (read-modify-write on state JSONB) producing lost or interleaved turns. This is the highest-severity risk in the design.

### 11. Budget placement: per-call in the endpoint service, cumulative on the session row

**Decision:** Keep the per-call budget in the endpoint service (today `AgentRunBudget{12, 60s}`, `agent_mcp_share.go:58-61`) and never push it into the executor. Add a cumulative cap: `total_steps` on the session row plus a `max_total_steps` (≈200), returning `budget_exceeded` when exceeded. Add `max_turns` and `expires_at` TTL with a reaper job mirroring `stale_run_reaper.go`.

**Rationale:** A per-call budget bounds one turn but not a session's cumulative work. Note that no per-key rate limit or budget exists today and per-key quotas are an explicit v1 non-goal; project budget enforcement and the executor's doom-loop detector (`executor.go:2134`) already exist.

### 12. Cancellation marks the session `interrupted`

**Decision:** A canceled turn may leave a half-written final event; mark the session `status='interrupted'` so `get_session` reflects it, and tolerate the partial event for v1.

**Rationale:** Correctness of the session status matters more than scrubbing the derived event, and the executor's next run re-derives state.

### 13. Agent/definition edited or deleted while keys are live

**Decision:** The agent definition is resolved at run time, so an edited definition is picked up (surprising but correct) and a deleted one fails closed via `ResolveDefinitionForAgent` / `resolveAgentShareTarget` (`agent_mcp_share.go:430-455`). Per-endpoint definition version pinning is an explicit non-goal.

**Rationale:** Pinning adds a version dimension to the endpoint schema for a case nobody has asked for; fail-closed on deletion is already the behavior.

### 14. Project share cleanup: tools only, no agent picker, retire `mcp-share-agent-scoping`

**Decision:** Remove the agent allowlist and agent picker from project share instances (`normalizeAgentAllowlist`, `share_instance.go:933-993`, the DTOs, and the templ agent picker). Retire the `mcp-share-agent-scoping` capability as a `## REMOVED Requirements` delta in this same change. Point users to the agent-scoped endpoint for agent sharing.

**Rationale:** Removes the "choosing an agent" ambiguity that motivated the change. Retiring the capability is not just dropping the picker: removing the allowlist also removes everything that capability required — agent-related tool filtering (`agent-list`/`agent-get`/`agent-list-available`), agent-execution gating, allowlist write validation, degrade-safely handling, name/ACP-slug reference resolution with fail-closed behavior, and the run-inspection/definition/discovery filtering that existed only to keep an agent-allowlisted instance within its allowlist. Leaving those requirements live would create spec drift, so they are removed together. All other project-share behavior (tool scoping, lifecycle, legacy shares) is unchanged.

## Risks / Trade-offs

- [Concurrent `continue_session` on one session] → Lost/interleaved turns from a read-modify-write on one ADK session. Mitigate with a per-session advisory lock or an optimistic `status='running'` CAS returning `ok:false, error:"session busy"`. **Highest risk.**
- [Runaway sessions] → The per-call budget bounds one turn but cumulative steps are unbounded. Mitigate with `total_steps` + `max_total_steps` (≈200) returning `budget_exceeded`. **Second-highest risk.**
- [Unbounded history growth] → Long-lived sessions grow the ADK state and event log without limit. Mitigate with an `expires_at` TTL, a `max_turns` cap, and a reaper job mirroring `stale_run_reaper.go`. **Third-highest risk.**
- [Budget placement] → Putting a cumulative cap in the executor would couple unrelated surfaces. Keep the per-call budget in the endpoint service; the cumulative cap lives on the session row.
- [Cancellation mid-run leaves a half-written final event] → Mark the session `status='interrupted'`; tolerate the partial event for v1.
- [Agent definition edited or deleted while keys are live] → An edited definition is picked up at run time; a deleted one fails closed. No version pinning in v1.
- [Session refs are LLM-visible] → Treat session ids as non-capabilities; re-authorize the key and assert `key_id` ownership on every session tool.
- [Token label overflow] → `core.api_tokens.name` is `varchar(255)`; the `"Agent MCP Key: "` prefix plus a long label would 500. Mirror `agentShareTokenName` truncation.
- [Migration ordering] → Must be sequential after `00148` and `00150`; `core.agent_mcp_shares` is only dropped in a later migration once no reader remains.
- [Gateway routes unreachable] → The agent-share handlers exist but are not registered in `main.go`; this change must register them or the feature has no UI.

## Migration Plan

1. Additive migration creating `agent_mcp_endpoints` + `agent_mcp_keys` + `agent_mcp_sessions`; mirror the DDL in `apps/server/internal/testutil/schema.sql`.
2. Same migration backfills: `INSERT` one endpoint per distinct `(project_id, agent_id)` from active `core.agent_mcp_shares`, then one key per share row copying `token_id`, `created_by`, `revoked_at`, with `label = name`. Tokens and scopes are untouched, so live keys keep working through the cutover.
3. Ship code that reads the new tables; keep `call_agent` byte-identical.
4. Drop `core.agent_mcp_shares` in a LATER migration once no reader remains.
5. **Rollback:** dropping the new tables restores the old path; tokens remain valid API tokens.

## Open Questions

- Whether to keep the old `GET .../mcp-shares` route names as aliases for the new key routes during a deprecation window, or cut over immediately.
- The exact reaper cadence and default session TTL (proposed: reaper cadence matching `stale_run_reaper.go`, TTL configurable, default 24h idle).
- Whether `max_total_steps` and `max_turns` are per-endpoint configuration or global constants in v1 (proposed: global constants for v1).
