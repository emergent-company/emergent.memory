## 1. Schema, backfill, and stores

- [x] 1.1 Add an additive migration after `00148`/`00150` creating `core.agent_mcp_endpoints`, `core.agent_mcp_keys`, and `core.agent_mcp_sessions` exactly as specified in design.md (partial unique indexes, cascades, label and ref uniqueness), and mirror the DDL in `apps/server/internal/testutil/schema.sql`; verify `task migrate:up` applies cleanly, a duplicate active endpoint for one agent is rejected, and a duplicate active label on one endpoint is rejected.
- [x] 1.2 Add the backfill in the same migration: one endpoint per distinct `(project_id, agent_id)` from active `core.agent_mcp_shares`, then one key per share row copying `token_id`, `created_by`, `revoked_at`, with `label = name`; verify a migration test asserts the counts match and that `token_id` values are unchanged.
- [x] 1.3 Add the Bun entity + store for endpoints (get by id, get active by agent, list by project, create, revoke) and verify unit tests cover each query and the active-only filter.
- [x] 1.4 Add the Bun entity + store for keys (get active by token id, list by endpoint, create, revoke, set revoke timestamp, update label) and verify unit tests cover active-only lookup and per-endpoint label uniqueness.
- [x] 1.5 Add the Bun entity + store for sessions (get by session ref, list by key, create, update counters/status/last-active, set expiry, reaper query for expired active sessions) and verify unit tests cover each query.

## 2. Auth refactor

- [x] 2.1 Implement `AuthorizeAgentEndpoint(ctx, apiTokenID, agentID) -> (endpoint, key, error)`: resolve the active key by `token_id` (else 403 "credential not bound"), resolve the active endpoint by `key.endpoint_id`, reject `endpoint.agent_id != agentID` (403), reject revoked/expired tokens (403), and resolve the agent rejecting `!agent.Enabled` (403); verify table-driven unit tests for every branch.
- [x] 2.2 Rewire `agent_endpoint_handler.go` and `routes.go` to `AuthorizeAgentEndpoint`, keeping the `mcp:agent-call` marker requirement and the project-transport rejection unchanged; verify handler tests assert the marker is still required and that a normal project key is rejected.
- [x] 2.3 Replace the single-share key CRUD routes with endpoint/key CRUD on the agent's own configuration surface (create key, list keys, revoke key, rotate key); verify route tests resolve each path and method.
- [x] 2.4 Keep `call_agent` behavior byte-identical (bare text reply, `agentRunErrorResult` error shape) while running through the new authorization; verify a back-compat test asserts the exact reply and error payloads.

## 3. Session store and executor continue path

- [x] 3.1 Add a session-aware run entry point in `domain/agents` — a NEW function that accepts `session_ref` and sets `ExecuteRequest.SessionID` — and do NOT overload the existing four-argument `RunAgentOnce`; verify unit tests with a fake executor assert `SessionID` is set and the session key is `session:<projectID>:<sessionRef>`.
- [x] 3.2 Wire the session store: create the session row before dispatch, update `turn_count`/`total_steps`/`last_run_id`/`last_active_at` on completion; verify unit tests assert the counters and timestamps on success and failure.
- [x] 3.3 Enforce per-session authorization on every session tool: re-authorize the key and verify `row.key_id == authorizedKey.id`, returning 404/403 otherwise; verify a test asserts another key's session ref never resolves.
- [x] 3.4 Implement per-session serialization: `pg_advisory_xact_lock(hashtextextended(session_ref,0))` around dispatch OR an optimistic `status='running'` CAS, returning `ok:false, error:"session busy"`; verify a concurrency test asserts two simultaneous continues do not interleave.
- [x] 3.5 Enforce cumulative budget and lifecycle: `total_steps` vs `max_total_steps` (≈200) → `budget_exceeded`; `max_turns` cap; `expires_at` TTL; mark `status='interrupted'` on cancellation; verify unit tests for each path.
- [x] 3.6 Add a reaper job mirroring `stale_run_reaper.go` that expires idle sessions; verify a test asserts expired sessions are marked and no longer continue.

## 4. Tool surface

- [x] 4.1 Replace the single static definition with the fixed five-tool catalog (`call_agent`, `start_session`, `continue_session`, `get_session`, `list_sessions`) in `tools/list`, with no per-endpoint picking; verify handler tests assert exactly those five tools and no project tools.
- [x] 4.2 Add the `tools/call` dispatch table; reject unknown tool names with a method/tool-not-found error and execute nothing; verify tests cover the rejection.
- [x] 4.3 Return the session tool results through `envelopeResult` (`ok`/`error`/`data`/`meta`, `meta {steps, run_id}`, `meta.kind` from `AgentRunError` kinds) and leave `call_agent` un-enveloped; verify tests assert session tool envelopes and the bare `call_agent` reply.
- [x] 4.4 Implement `start_session` semantics (message present → run turn 1 and return the reply; absent → create an empty session and return only `session_id`) and `continue_session` semantics (append to prior context, return the new reply); verify tests for both start variants and for continue.

## 5. Project share cleanup and capability retirement

- [x] 5.1 Remove `normalizeAgentAllowlist` (`share_instance.go:933-993`) and the `allowed_agents` plumbing from the project instance model, store, service, and handler; verify instance tests assert tool scoping still works and no agent filtering occurs.
- [x] 5.2 Delete the agent-allowlist enforcement code paths: the `InstanceDeniesTool` agent gating, `agentDeniedByAllowlist`, and `filterAgentResult`, plus the run-inspection, agent-definition read, and agent-discovery filtering that existed only to enforce an instance agent allowlist; verify no call site still references these helpers and that tool scoping and token scopes alone govern tool availability.
- [x] 5.3 Delete the tests that covered the agent allowlist (discovery filtering, execution gating, write validation/definition resolution, degrade-safely, forced-empty discovery) in the same commit, so no test asserts removed behavior; verify `go test ./domain/mcp/... -short` is green.
- [x] 5.4 Record the capability retirement: ship the `mcp-share-agent-scoping` `## REMOVED Requirements` delta with this change; verify `openspec validate agent-scoped-mcp-endpoint --strict` is valid and reports no drift.
- [x] 5.5 Remove the `agents` field from the create/update/list DTOs and the agent picker from the gateway templ; verify no handler reads or writes an agent allowlist and a request supplying `agents` is rejected.
- [x] 5.6 Point users to the agent-scoped endpoint for agent sharing in the project-share UI copy; verify all other project-share behaviour tests remain green.

## 6. Gateway UI

- [ ] 6.1 Register the currently-unreachable agent-share routes in the gateway `main.go` (mirroring the registered project-share routes) and run `templ generate`; verify the pages load and `go build ./...` passes from `gateway/`.
- [ ] 6.2 Add key management UI on the agent's own configuration surface: list keys, create a labeled key with a one-time secret reveal, revoke, and rotate; verify HTMX flows and that the secret is shown exactly once.
- [ ] 6.3 Add session visibility (list sessions per endpoint/key with status, turn count, last active); verify the view renders for an endpoint with and without sessions.

## 7. Verification

- [ ] 7.1 Run `gofmt -l .`, `go build ./...`, and `go test ./domain/mcp/... ./domain/agents/... ./internal/... -short` from `apps/server`; all green.
- [ ] 7.2 Run `golangci-lint run ./domain/mcp/... ./domain/agents/...` and resolve new findings.
- [ ] 7.3 Run the gateway checks (`templ generate`, `go build ./...`, `task lint`) from `apps/web-ui`; all green.
- [ ] 7.4 Add a domain-level end-to-end test: create an endpoint key → connect to `/api/mcp/agents/:agentId` → `tools/list` shows the five session tools → `call_agent` returns bare text → `start_session` then `continue_session` shares context → `get_session`/`list_sessions` reflect the session → revoke the key → the key is rejected.
- [ ] 7.5 Add a concurrency regression test that two simultaneous `continue_session` calls on one session yield one success and one `session busy`, and a budget test that a session exceeding `max_total_steps` returns `budget_exceeded`.
