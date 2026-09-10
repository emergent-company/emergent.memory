## Context

See proposal.md — Why. The backend already runs a unified MCP JSON-RPC server at `/api/mcp` (`domain/mcp/routes.go`, `streamable_http_handler.go`) with `initialize`/`tools/list`/`tools/call`, and it authenticates via `X-API-Key`/Bearer into an `auth.AuthUser` carrying `Scopes`, `ProjectID`, and `APITokenID` (`pkg/auth/middleware.go`). Agent execution is blocking by default: `executor.Execute(ctx, ExecuteRequest)` (`domain/agents/executor.go:330`) returns an `ExecuteResult` with `RunID/Status/Summary/Steps` but **no reply text**. The only existing path that returns assistant text synchronously is the ACP sync path (`acp-trigger-run` → `RunToACPObject`), which reads persisted run messages. There is no per-agent credential: `core.api_tokens` has only scopes, and there is no per-agent endpoint.

The in-flight `add-mcp-share-instances` change adds project-level share instances (`core.mcp_share_instances`, migration `00144`), which this change stacks on for migration ordering.

## Goals / Non-Goals

**Goals:**
- A dedicated, agent-scoped MCP surface with a single tool and a credential bound to that agent.
- Reuse the existing JSON-RPC handling, auth middleware, token lifecycle, and run-message extraction rather than duplicating them.
- Keep the project MCP endpoint and share instances untouched.

**Non-Goals:**
- Async result delivery or polling (v1 is synchronous one-shot).
- Conversation continuity / session pinning across calls (v1 is stateless).
- Per-share rate limits, quotas, or streaming progress (deferred).
- Exposing more than one tool per agent.

## Decisions

### 1. Dedicated endpoint path `/api/mcp/agents/:agentId`

**Decision:** Add a new route group (auth-gated) rather than adding a synthetic tool to the project catalog.

**Rationale:** A per-agent URL gives a clean identity and a genuine one-tool catalog; the key is unambiguously for one agent; and it avoids polluting the project endpoint's tool list. Alternatives considered: a synthetic `call_agent` tool on the project endpoint scoped by the instance model (less clear identity, one catalog for all) and a new URL scheme with its own protocol (rejected — reuse JSON-RPC).

### 2. New `core.agent_mcp_shares` table binding `token_id` → `agent_id`

**Decision:** Persist the binding in a dedicated table (id, project_id, agent_id, token_id, name, description, created_by, timestamps, revoked_at).

**Rationale:** `core.api_tokens` has no metadata column, and the binding needs list/revoke/rotate semantics plus name uniqueness. A dedicated table keeps agent-share semantics independent of the tool-allowlist instance model. Alternatives: reuse `core.mcp_share_instances` with a kind discriminator (couples two features) or add a JSONB column to tokens (awkward lifecycle queries).

### 3. Agent-scoped handler reusing the JSON-RPC helpers, not the project handler

**Decision:** Implement an agent-endpoint handler that shares the request/response/error types and the `streamable_http_handler` session mechanics, but substitutes a fixed single-tool provider and a dedicated call executor. `tools/list` returns the one `call_agent` definition derived from the bound agent.

**Rationale:** Avoids duplicating JSON-RPC framing and avoids weakening the project catalog's scope/instance filtering.

### 4. Synchronous, stateless execution with a bounded budget

**Decision:** `call_agent` starts one run via the executor, waits for completion, then reads the run's persisted messages to extract the assistant reply. Apply a maximum step count and timeout. Ignore/park conversation continuity.

**Rationale:** MCP `tools/call` is request/response and the client timeout is finite; a bounded blocking run is the simplest correct v1. Alternatives: async + poll (needs a second tool and inspection tools, currently blocked by shares) and session-continuous runs (needs a session contract) — both deferred.

### 5. Reply extraction via persisted run messages

**Decision:** Reuse the run-message read path and ACP text extraction (`FindMessagesByRunID`, `memoryMessagesToACP`/`RunToACPObject`) to obtain the assistant text; a new `RunAgentOnce`-style helper returns `(reply, runID, error)`.

**Rationale:** This is the only existing mechanism that yields assistant text from a run; reusing it avoids changing `ExecuteResult`.

### 6. Authorization = active token bound to the URL's agent

**Decision:** The handler resolves the share by `(AuthUser.APITokenID, agentId)` and requires the token to be active; mismatch → 403, missing/invalid token → 401 (middleware).

**Rationale:** One credential, one agent; fail-closed; the token's project is already enforced by the middleware's project resolution.

### 7. Marker scope isolates share credentials from the project MCP endpoint

**Decision:** Mint every per-agent share token with the dedicated scope `mcp:agent-call` (plus the legacy read-only MCP set so the agent's internal loopback tool calls keep working). All project MCP transports (`handler.go` JSON-RPC, `streamable_http_handler.go`, `sse_handler.go`) reject any credential carrying `mcp:agent-call` with HTTP 403 before listing or executing. The per-agent endpoint requires the marker scope.

**Rationale:** Agent tools have an empty `RequiredScope`, so without a marker a share key would pass scope filtering at `/api/mcp` and could call `trigger_agent(agent_name=B)` for any agent in the project. A marker makes the credential valid at exactly one surface. Normal project tokens never carry it, so they are unaffected.

### 8. Reply extraction filters raw message roles, not ACP roles

**Decision:** `RunAgentOnce` extracts the reply from persisted RAW run messages whose `role == "assistant"`, not from `memoryMessagesToACP` output. If no non-empty assistant text exists it returns a structured `AgentRunError` (never an empty success).

**Rationale:** `memoryRoleToACP` maps both `system` and `tool_result` to `agent`, so filtering on the mapped role would leak system prompts or raw tool output as the agent's reply. Filtering raw rows is the only safe boundary.

## Risks / Trade-offs

- [Client `tools/call` timeout shorter than a long agent run] → Cap steps and execution time; return a timeout error rather than hanging. Document the limit; revisit async later.
- [Human-in-the-loop pause during a run] → Treat a paused/`input-required` run as a structured error carrying the question; never fabricate a reply.
- [Blocking run ties up a server goroutine] → Server timeouts are long; the per-call budget bounds it. Monitor; add concurrency limits later.
- [Reply extraction drift as run-message shape evolves] → Reuse the existing ACP extraction helpers and cover with tests; centralize the helper.
- [Migration ordering with the in-flight share change] → This branch stacks on `feat/mcp-share-instances`; use `00145` so both merge in order.
- [Token name overflow] → `core.api_tokens.name` is `varchar(255)`; the `"Agent MCP Share: "` prefix plus a long share name would 500. `agentShareTokenName` truncates to fit.
- [Two active shares bound to one token] → A partial `UNIQUE (token_id) WHERE revoked_at IS NULL` index prevents one credential from authorizing two agents at once; revoked rows release the token.

## Migration Plan

1. Add `00145_create_agent_mcp_shares.sql` (additive) and mirror the DDL in `internal/testutil/schema.sql`.
2. Ship the share endpoints and the agent endpoint together.
3. **Rollback:** remove the routes/handler and drop the table; tokens minted for shares remain valid API tokens but no longer authorize the agent endpoint.

## Open Questions

- Tool name `call_agent` vs `send_message`; v1 uses `call_agent`. Renaming later is a spec change but cheap (single static definition).
- Whether to accept ACP slugs in the URL in addition to agent UUIDs; v1 uses the agent UUID.
