## 1. Scaffold

- [ ] 1.1 Create `ui/` Go module with `go.mod` (replace directive → local `go-daisy`), deps `github.com/emergent-company/go-daisy`, `a-h/templ`, `labstack/echo`
- [ ] 1.2 Set up the echo server and a templ layout shell (daisyUI theme + nav), serving on `:8090` with API base URL + key from env

## 2. API client

- [ ] 2.1 Define Go structs mirroring the control-plane models (`Agent`, discriminated `Backend` openai_compat/realtime/a2a, `McpServer`, `Tools`, `SubAgentRef`, `McpToolRef`)
- [ ] 2.2 Implement an HTTP client with `X-API-Key` header (base URL + key from env) and error mapping for 401/404/409/422

## 3. Agents UI

- [ ] 3.1 Agent list page (name, enabled state, backend type, version)
- [ ] 3.2 Create-agent form (name, system prompt, backend discriminator)
- [ ] 3.3 Edit-agent page (full replace: backend, tools, sub-agents)
- [ ] 3.4 Delete agent with confirmation
- [ ] 3.5 Activate/deactivate toggle
- [ ] 3.6 Agent status view (enabled, version, sub-agent references)

## 4. MCP servers UI

- [ ] 4.1 MCP server list page
- [ ] 4.2 MCP server create/edit form (name, URL, transport, tool allowlist)
- [ ] 4.3 MCP server delete

## 5. Auth + errors

- [ ] 5.1 Surface 401 (invalid key), 409 (name conflict), and 422 (validation) errors in the UI

## 6. Build + verify

- [ ] 6.1 Run `templ generate` and `go build ./...`
- [ ] 6.2 Smoke test against the live API: agent list/create/edit/delete + MCP server create
