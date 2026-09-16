## Why

Today a project can expose MCP access only by minting a one-off read-only token with a fixed scope set (`POST /api/projects/:projectId/mcp/share`). Every share grants the same coarse scopes and exposes the entire scope-filtered tool catalog, so an outside agent either gets more access than intended or none at all. Teams need multiple named, independently scoped MCP access grants — each limited to an explicit set of memory tools and, optionally, a chosen set of agents.

## What Changes

- Introduce a first-class, project-scoped **MCP share instance**: a named record that binds one API token to an explicit tool allowlist and an optional agent allowlist.
- Enforce the allowlist at **both** `tools/list` and `tools/call`, so an instance only sees — and can only invoke — the tools it was granted, on top of existing scope filtering.
- Let a share expose a chosen subset of **agents**: agent-related tools (`agent-list`, `agent-get`, `agent-list-available`) return only the instance's allowed agents, and agent execution is rejected for agents outside the allowlist.
- Add CRUD endpoints under `/api/projects/:projectId/mcp/shares` to create, list, get, update, and revoke instances, plus an endpoint to rotate an instance's token.
- Add a tool-catalog endpoint `GET /api/projects/:projectId/mcp/tools` that returns the includable memory tools (name, description, required scope, category) so a management UI can render an allowlist picker.
- Reuse the existing `apitoken` primitive for credentials: an instance token is a normal project API token whose scopes are the union of the selected tools' required scopes plus `projects:read`; the instance links to the token by ID.
- Keep the existing `POST /api/projects/:projectId/mcp/share` working. Its tokens surface as **legacy instances** with no explicit allowlist (all scope-permitted tools) and stay subject to automatic scope filtering. **No breaking change** to the MCP JSON-RPC contract or existing clients.

## Capabilities

### New Capabilities

- `mcp-share-instances`: persistence and lifecycle (create/list/get/update/revoke/rotate) of named, project-scoped MCP share instances bound to an API token.
- `mcp-share-tool-scoping`: per-instance tool allowlist enforced for both `tools/list` and `tools/call`.
- `mcp-share-agent-scoping`: per-instance agent allowlist limiting agent-related tools to the selected agents.
- `mcp-tool-catalog-api`: endpoint exposing the includable memory tool catalog (with required scopes) for management UIs.

### Modified Capabilities

<!-- None: existing MCP and token specs keep their requirements; the new
     capabilities layer on top of scope filtering and token creation. -->

## Impact

- **New migration** `apps/server/migrations/00144_add_mcp_share_instances.sql` (new `core.mcp_share_instances` table and indexes).
- **`apps/server/domain/mcp/`**: new `share_instance.go` (model, store, service), `share_instance_handler.go` (CRUD + catalog routes); updates to `handler.go`, `streamable_http_handler.go`, `sse_handler.go` (resolve instance from `AuthUser.APITokenID` and filter tools/agents), `service.go` (allowlist filter, agent scoping), `routes.go`.
- **`apps/server/domain/mcp/share.go`**: legacy `/share` flow retained; created tokens are recognized as legacy instances.
- **`apps/server/domain/apitoken/`**: no schema change; reuse `Service.Create` and `Service.Revoke`.
- **Consumed by** the gateway/UI change `add-mcp-share-management` in `memory.web-ui` (this repo's admin UI is a separate repo; the HTTP API defined here is its contract).
