## 1. Server — persistence + required config

- [x] 1.1 Migration `apps/server/migrations/00223_add_required_config_to_mcp_servers.sql`: add `required_config jsonb NOT NULL DEFAULT '[]'` (+ down)
- [x] 1.2 `mcpregistry/entity.go`: `RequiredConfigKey` type; `MCPServer.RequiredConfig`; extend `MCPServerDTO` (`requiredConfig`, `needsConfiguration`, `missingConfigKeys`); extend `InstallResultDTO`
- [x] 1.3 `mcpregistry/service.go`: `collectRegistryRequiredConfig`, `missingRequiredConfigKeys`, persist on create (incl. `CreateMCPServerDTO.RequiredConfig`), compute DTO fields in `ToDTO`
- [x] 1.4 `buildRemoteDTO`/`InstallFromRegistry`: record selected-remote required config, create disabled when non-empty, skip discovery
- [x] 1.5 `UpdateServer`: `ErrRequiredConfigMissing` when enabling with missing keys; handler maps it to `400`

## 2. Server — save-less test endpoint

- [x] 2.1 `Service.TestServerConfig(ctx, projectID, dto, serverID)`: transient server, draft plaintext secrets, edit-mode stored-secret backfill, reject device target, delegate to `ProxyManager.InspectServer`
- [x] 2.2 `Handler.TestServerConfig` + route `POST /api/admin/mcp-servers/test` in `routes.go`
- [x] 2.3 Server unit tests: required-config collection (GitHub fixture), missing-key rule, install-disabled + result keys, enable rejection, transient-server test (no write), edit-mode backfill, device rejected

## 3. Web UI client + routes

- [x] 3.1 `extras.go`: `MCPServer` wire fields + `MCPRequiredConfigKey`; `TestMCPServer`
- [x] 3.2 `backend.go` interface + `main.go` route `POST /api/mcp-servers/test`
- [x] 3.3 `mcp_servers_handlers.go`: test proxy handler; enable-gate `400` surfaces via toggle JSON
- [x] 3.4 `mcp_registry.go`: redirect needing-config install to `/{id}/edit?configure=1`

## 4. Web UI — rendering

- [x] 4.1 List (`mcp_servers.templ`): "Needs configuration" badge + alert naming keys + "Configure" action
- [x] 4.2 Edit/create form: required-row marker, `?configure=1` banner, never-configured secret placeholder
- [x] 4.3 Form data plumbing (`mcp_servers.go`): `RequiredConfigKeys`/`MissingConfigKeys` on `mcpServerFormData`; required/missing flags on rows
- [x] 4.4 Test connection button + JS serializer + result rendering via existing `renderInspect`
- [x] 4.5 Web UI unit tests: badge render, required-row marker, test endpoint proxy, install redirect

## 5. Verify

- [x] 5.1 `templ generate` (web-ui; generated output is gitignored)
- [x] 5.2 `go build ./...` in `apps/server` and `apps/web-ui/gateway`
- [x] 5.3 `go test ./...` in both modules (server `domain/mcpregistry`; gateway all packages)
- [x] 5.4 `golangci-lint run` clean in both modules
- [ ] 5.5 Playwright e2e — not run in this environment: the existing `mcp-servers-create-ui` spec dials a live memory backend (`E2E_MCP_EXAMPLE_URL`); no stack available in the worktree. UI affordances are covered by gateway unit tests.
- [ ] 5.6 Server restart + browser check — deferred to the review environment.
- [x] 5.7 `openspec validate mcp-required-config --strict`
