## 1. Schema and model

- [x] 1.1 Add migration `apps/server/migrations/00144_add_mcp_share_instances.sql` creating `core.mcp_share_instances` (`id`, `project_id`, `name`, `description`, `token_id` FK to `core.api_tokens`, `allowed_tools text[]`, `allowed_agents uuid[]`, `is_legacy bool`, `created_by`, `created_at`, `updated_at`, `revoked_at`) plus a partial unique index on `(project_id, lower(name)) WHERE revoked_at IS NULL`, and mirror the same DDL in `internal/testutil/schema.sql` (tests do not run migrations); verify `task migrate:up` applies cleanly and a duplicate active name is rejected.
- [x] 1.2 Add the `MCPShareInstance` Bun entity and store in `apps/server/domain/mcp/share_instance.go` with list/get/create/update/revoke/rotate queries; verify unit tests cover retrieval by `token_id` returning the bound instance and by `(project_id, id)`.

## 2. Instance lifecycle and token binding

- [x] 2.1 Implement `ShareInstanceService.Create` that validates the name, de-duplicates/validates `tools` and `agents`, derives token scopes from the selected tools' required scopes plus `projects:read`, mints the token via `apitoken.Service.Create`, and persists the instance; verify unit tests assert the derived scope set and a one-time returned token.
- [x] 2.2 Implement `List`/`Get`/`Update`, where `Update` re-derives and persists token scopes via the existing scope-update path without changing the token secret; verify unit tests assert scopes follow the allowlist and the token value is unchanged.
- [x] 2.3 Implement `Revoke` (idempotent, revokes bound token) and `Rotate` (new token with current scopes, old token revoked); verify unit tests assert revoked tokens fail lookup and rotate preserves the allowlist.
- [x] 2.4 Reject empty `tools` (`[]`), duplicate names, unknown tools, unknown/non-project agents, and `AgentOnly` tools with 409/422 responses; verify table-driven unit tests cover each error mapping.

## 3. Tool allowlist enforcement

- [x] 3.1 Add instance resolution from `AuthUser.APITokenID` and an `InstanceScope` value carrying `AllowedTools`/`AllowedAgents`; verify unit tests cover present instance, absent instance (legacy → null allowlist), and revoked instance.
- [x] 3.2 Add `FilterToolsForInstance` in the `mcp` service and apply it after `FilterToolsForScopes` in `handler.go`, `streamable_http_handler.go`, and `sse_handler.go` `tools/list`; verify unit tests assert non-allowlisted tools are hidden and scope filtering is not weakened.
- [x] 3.3 Enforce the allowlist in `tools/call` before execution (deny-by-default) in all three transports; verify a unit/API test asserts a non-allowlisted but scope-permitted tool call returns a structured error and causes no side effect.

## 4. Agent allowlist enforcement

- [x] 4.1 Filter agent-discovery tools (`agent-list`, `agent-get`, `agent-list-available`) by the instance's `allowed_agents`; verify unit tests assert non-allowed agents are omitted and `agent-get` for a non-allowed agent returns not-found.
- [x] 4.2 Gate agent-execution tools by `allowed_agents` before the run starts; verify a unit test asserts a non-allowed agent invocation is rejected with no run created.
- [x] 4.3 Handle a deleted allowed agent gracefully; verify a unit test asserts the instance still lists remaining agents without error.
- [x] 4.4 Ensure an agent allowlist with `null` exposes exactly the scope-permitted agents; verify a unit test asserts null-allowlist behavior.

## 5. Tool catalog endpoint

- [x] 5.1 Implement `GET /api/projects/:projectId/mcp/tools` returning includable tools (name, description, requiredScope, category), excluding `AgentOnly`, ordered by category then name; verify a unit test asserts ordering and exclusion.
- [x] 5.2 Enforce project-admin access and clear error statuses (403/404) rather than an empty list; verify handler tests cover admin, non-admin, and unknown project.

## 6. Routes, legacy compatibility, and verification

- [x] 6.1 Register the CRUD, rotate, and catalog routes in `mcp/routes.go`; verify route tests resolve each path and method.
- [x] 6.2 Represent tokens from the legacy `POST /:projectId/mcp/share` flow as legacy instances (null allowlist); verify a test asserts a legacy token still authenticates and lists all scope-permitted tools.
- [x] 6.3 Run `task lint` and `task test` in `apps/server`; fix all failures, and confirm the new package builds.
- [x] 6.4 Add an end-to-end API test that creates an instance with a 2-tool allowlist, connects over `/api/mcp` with its key, asserts only those tools are listed, asserts a non-allowlisted call is rejected, then revokes and asserts the key is rejected.
