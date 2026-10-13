## 1. Server — per-agent .mcpb bundle route

- [x] 1.1 Add `GET /api/mcp/agents/:agentId/bundle?token=<emt_*>` as a public route (registered before the auth-gated `/api/mcp/agents` group), mirroring `/api/mcp/bundle`
- [x] 1.2 Handler: resolve the raw token to an `api_tokens.id` (SHA-256 hash lookup, active/non-expired), then `AuthorizeAgentEndpoint(ctx, tokenID, agentID)` to require an active agent key bound to that agent; reject otherwise
- [x] 1.3 Build the bundle with `mcpURL = base + "/api/mcp/agents/" + agentID` (reuse `buildMCPBundleZIP`), filename from the project slug; serve as an attachment download
- [x] 1.4 Swagger annotations + regenerate `apps/server/docs/swagger/*`
- [x] 1.5 Tests: valid key → zip download; revoked/expired token → 401; key bound to a different agent → 403; unknown agent → 4xx; malformed/missing token → 400

## 2. Gateway — external origin for MCP docs

- [x] 2.1 Add `externalMCPURL(u)`: swap scheme+host of a backend URL for the memory server's public origin (`serverPublicBaseURL()`: `MEMORY_PUBLIC_URL`, defaulting to `MEMORY_URL`), preserve path/query, return the input on parse failure or an unconfigured origin
- [x] 2.2 Apply in `agent_mcp_endpoint_handlers.go`: create/rotate key reveal (rewrite `secret.MCPURL` before snippets), `loadAgentMCP`, JSON get/create endpoint, create/rotate key
- [x] 2.3 Apply in `mcp_shares_handlers.go`: `mcpShareRevealFromCreated`, `mcpShareCreatedJSON`
- [x] 2.4 Tests: a backend `mcpUrl` on the internal host renders/echoes as the configured memory-server public origin, explicitly not `PUBLIC_BASE_URL`

## 3. Gateway — .mcpb download links

- [x] 3.1 Add a "Download Claude Desktop bundle" link to the project-share reveal → `serverPublicBaseURL() + "/api/mcp/bundle?token=" + escape(token)`
- [x] 3.2 Add the same to the agent-endpoint reveal → `serverPublicBaseURL() + "/api/mcp/agents/<id>/bundle?token=" + escape(token)`
- [x] 3.3 `templ generate`; tests assert the link host is the memory-server external origin

## 4. Verification

- [x] 4.1 `apps/server`: `go build ./...`, `go test ./domain/mcp/...` (incl. DB-backed integration against Postgres), route-authority guard, golangci-lint on touched packages
- [x] 4.2 `apps/web-ui/gateway`: `templ generate`, `go build ./...`, `go test ./...`, `task lint`
- [x] 4.3 `openspec validate mcp-external-origin-and-desktop-bundle --strict`
- [x] 4.4 Automated cross-surface check: gateway render tests assert the external host and the `.mcpb` links; the server DB integration test asserts the downloaded bundle manifest's MCP URL targets the per-agent endpoint (a live browser click-through against a deployed build remains a post-deploy manual step)
