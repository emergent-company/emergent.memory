## 1. Schema and share store

- [x] 1.1 Add migration `apps/server/migrations/00145_create_agent_mcp_shares.sql` creating `core.agent_mcp_shares` (`id`, `project_id`, `agent_id`, `token_id` FK to `core.api_tokens`, `name`, `description`, `created_by`, `created_at`, `updated_at`, `revoked_at`) with a partial unique index on `(project_id, lower(name)) WHERE revoked_at IS NULL` and indexes on `token_id` and `(project_id, agent_id)`; mirror the DDL in `internal/testutil/schema.sql`; verify migration applies and a duplicate active name is rejected.
- [x] 1.2 Add the Bun entity + store (get by id/token, list by project/agent, create, update, revoke) and verify unit tests cover each query.

## 2. Share credential lifecycle

- [x] 2.1 Implement create: validate admin + agent exists in project, mint a token via `apitoken.Service.Create`, persist the binding, return share + per-agent `mcpUrl` + one-time token; verify unit tests for success and the one-time token.
- [x] 2.2 Implement list (per-agent and project-wide) returning no token secret, and default naming; verify tests assert status/timestamps and absence of the token.
- [x] 2.3 Implement revoke (idempotent, revokes the bound token) and rotate (atomic replacement, same binding, one-time new token); verify tests assert revoked tokens fail and rotation preserves binding.
- [x] 2.4 Enforce admin and project scoping on every endpoint, and reject unknown/foreign agent ids; verify handler tests for 403/404/409/422 cases.

## 3. Run-once execution helper

- [x] 3.1 Add a `RunAgentOnce(ctx, projectID, agentID, message)` helper that runs the agent synchronously with capped max-steps/timeout and returns `(reply string, runID string, err error)` by reading persisted run messages and extracting agent-authored text (the executor persists assistant turns under the sanitized agent name); verify unit tests with a fake executor for success, failure, and paused runs.
- [x] 3.2 Map run failure, paused/input-required, and agent missing/disabled to distinct structured errors; verify tests cover each.

## 4. Per-agent MCP endpoint

- [x] 4.1 Add the auth-gated route `/api/mcp/agents/:agentId` (initialize/tools list/call) and an agent-scoped handler returning exactly one `call_agent` tool definition derived from the bound agent; verify handler tests assert one tool listed and unknown tools rejected.
- [x] 4.2 Implement `call_agent` to require `message`, resolve the token→agent binding (403 on mismatch), call `RunAgentOnce`, and return the reply text; verify tests for success, missing message, unbound/foreign token, and no-credential.
- [x] 4.3 Return structured `isError` tool results for run failure, pause, and unavailable agent; verify tests assert error results and no fabricated reply.
- [x] 4.4 Register the routes in `domain/mcp/routes.go` after the auth middleware and confirm unauthenticated requests are rejected (401).

## 5. Verification

- [x] 5.1 Run `gofmt -l .`, `go build ./...`, and `go test ./domain/mcp/... ./domain/agents/... ./internal/... -short` from `apps/server`; all green.
- [x] 5.2 Run `golangci-lint run ./domain/mcp/... ./domain/agents/...` and resolve new findings.
- [x] 5.3 Add a domain-level end-to-end test: create a share → connect to the per-agent endpoint with the key → `tools/list` shows only `call_agent` → `call_agent` returns the reply → revoke → key rejected.
