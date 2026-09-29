## 1. Server — builtin tool groups endpoint (agents)

- [x] 1.1 Extract the catalog→groups core out of `AgentDefinition.ToolGroupsWithCatalog` into a shared `catalogGroupMembership` helper; add `ToolGroupsFromCatalog` (catalog-only, `enabled: true`, `policy: ""`, frozen group order, empty groups omitted) without changing the existing agent path.
- [x] 1.2 Add `Handler.ListBuiltinToolGroups` and route `GET /api/admin/builtin-tool-groups` (middleware trio) returning the standard success envelope with `[]ToolGroupDTO`.
- [x] 1.3 Unit tests: order preserved, empty groups omitted, `enabled`/`policy` defaults.

## 2. Server — per-tool invocation (mcpregistry)

- [x] 2.1 Add `ProxyManager.CallToolOnServer` (by server row, no name-prefix inference) reusing `getOrConnect` + `convertCallToolResult`.
- [x] 2.2 Add `Service.CallToolOnServer` resolving the server by id within the project, enforcing `server.Enabled` + tool existence + `tool.Enabled`, routing builtin to `mcp.Service.ExecuteTool` and external to the proxy.
- [x] 2.3 Add `Handler.CallToolOnServer` + route `POST /api/admin/mcp-servers/:id/tools/:toolName/call`; body `{"arguments": {…}}`, response `data` = `*mcp.ToolResult`.
- [x] 2.4 Typed errors → 404 (missing server/tool), 409 (server/tool disabled), 403 (forbidden), 502 (upstream, sanitized).
- [x] 2.5 `mcp.Service.AuthorizeToolCall` — transport-parity per-tool authority (`AgentOnly` refused, `SuperadminOnly` platform grant, `RequiredScope` against caller scopes), replacing blanket `TrustedInternal`; dispatch with `ContextWithTransportEnforced`.
- [x] 2.6 Bound the call with a timeout (30s) and log upstream detail server-side only.
- [x] 2.7 Tests: builtin delegation, agent-only rejection, scope denial, superadmin denial, disabled server/tool, not-found, args forwarding, sanitized 502, context-deadline abort.

## 3. Gateway — identity, grouping, invoke

- [x] 3.1 Render the builtin server as "Memory tools" without the duplicated `builtin` badge; keep the row read-only for configuration.
- [x] 3.2 Load and render the builtin capability groups (`ListBuiltinToolGroups`), matching the agent picker's group visual language; keep uncovered tools in an "Other" group; degrade to the flat list when the endpoint fails.
- [x] 3.3 Add `CallMCPServerTool` client + `POST /api/mcp-servers/:id/tools/:toolName/call` proxy route; validate the arguments body (400 on non-object).
- [x] 3.4 Add the per-tool "Run" dialog + result rendering built with DOM nodes/`textContent` (never `innerHTML`); inline validation for invalid JSON.
- [x] 3.5 Gateway tests for identity, grouped render + Other fallback, invoke affordances, proxy handler (200/400/upstream), loader group fetch/degrade, client methods.

## 4. Spec + verify

- [x] 4.1 Add the OpenSpec change artifacts (proposal + `mcp-tool-invocation`, `mcp-builtin-tool-groups`, `mcp-servers-ui` deltas).
- [x] 4.2 Server: `cd apps/server && go build ./...` and `go test ./domain/agents/... ./domain/mcpregistry/...` (DB-backed suite skips without Postgres).
- [x] 4.3 Gateway: `cd apps/web-ui && task generate` then `cd apps/web-ui/gateway && go build ./... && go test ./...`.
- [x] 4.4 `task lint` clean for the touched trees.
- [ ] 4.5 Manual UI check of the builtin row, grouped tools, and the invoke dialog on the dev server.

<!-- Manual 4.5 pending: no browser session is reachable from the build host. Automated
     page/render tests cover the builtin identity ("Memory tools"), the grouped render with
     the "Other" fallback, the invoke affordances/dialog, and the proxy handler states;
     the live click-through is left for PR review. -->

