# Tasks — add-mcp-registry-install

TDD throughout: each implementation task ships with the unit test named beside
it. Build/test per module (`apps/server/pkg/sdk`, `apps/connector.linux`,
`apps/web-ui/gateway`); the Mac app builds and tests on `mcj-mini`.

## 1. SDK registry proxy methods

- [x] 1.1 Add registry DTOs to `apps/server/pkg/sdk/mcpregistry` mirroring the
  admin payload (`RegistryServer`, `RegistryPackage`, `RegistryRemote`,
  `RegistryEnvVar`, `RegistrySearchResult`) with tests for JSON decode of a
  fixture entry.
- [x] 1.2 `SearchRegistry(ctx, query, limit, cursor)` → `GET
  /api/admin/mcp-registry/search` (project header preserved). httptest test:
  query/limit/cursor forwarded, response decoded.
- [x] 1.3 `GetRegistryServer(ctx, name)` → `GET
  /api/admin/mcp-registry/servers/{name}` with the name path-escaped. httptest
  test: slash-containing name escaped, response decoded, upstream error
  surfaced.

## 2. Connector registry translation (`apps/connector.linux/internal/mcpregistry`)

- [x] 2.1 `SuggestConfig(entry, localNameOverride) (mcphost.ServerConfig,
  Suggestion, error)` — stdio-first selection + remote fallback. Table tests:
  npm → `npx -y <id>@<ver>`, pypi → `uvx <id>` (pinned), remote `http`,
  remote `sse`, unsupported (neither).
- [x] 2.2 Argument mapping: `runtimeArguments` then `packageArguments` appended
  in order, empty/optional values omitted. Table tests.
- [x] 2.3 Env/header placeholder extraction with required/secret flags; empty
  values in the suggested config. Tests.
- [x] 2.4 Runner availability probe (`npx`/`uvx` on `PATH`, injectable for
  tests): missing runner yields a warning, not an error. Tests.
- [x] 2.5 Derived local name from the registry name (`io.github.x/y` → `y`),
  with override. Tests.

## 3. Connector management API + wiring

- [x] 3.1 Define a registry-source interface in `internal/mgmtapi` and inject it
  via `NewServer` (nil-safe: routes return a clear "registry unavailable"
  error when the connector has no Memory client). Handler tests with a stub
  source.
- [x] 3.2 `GET /api/mcp-registry/search?q=&limit=&cursor=` proxies the source
  and returns results unchanged. Handler tests (passthrough, upstream error →
  502/error body).
- [x] 3.3 `GET /api/mcp-registry/servers/{name}` proxies the source and returns
  `{ server, suggested, envVars, warnings }`; name path-escaped upstream.
  Handler tests (suggestion present, unsupported entry, upstream 404).
- [x] 3.4 Wire a registry source built from the connector config
  (`memoryapi.NewProjectClient(cfg.ServerURL, cfg.Token)`) in
  `cmd/memory-connector/relay.go`; leave routes disabled when no token/project.
  Test the construction/guard where practical.

## 4. Mac app — browse registry + prefilled install

- [x] 4.1 Registry client (`apps/connector.mac`): `search(query)` and
  `registryServer(name)` against the loopback API, plus decode models matching
  3.3. Swift unit tests for decode (suggested stdio, suggested remote, error).
- [x] 4.2 `MCPServerForm` prefill initializer from the suggested config
  (transport, command/args/env or url/headers, name). Unit tests for stdio and
  remote prefill.
- [x] 4.3 Browse UI: a "Browse registry" affordance on `MCPServersPage` opening
  a searchable list; selecting an entry loads the suggestion and opens
  `AddMCPServerSheet` prefilled, surfacing required/secret env and any warning.
  View/logic tests where the harness allows.
- [x] 4.4 Confirm the Add path uses the existing create client unchanged;
  verify secrets remain local (no Memory upload) by construction.

## 5. Web UI — registry browse/install page

- [x] 5.1 Gateway client methods on `MemoryClient` for
  `/api/admin/mcp-registry/search` and `/install`. Client tests.
- [x] 5.2 `GET /settings/mcp-registry` list page: search box + results with
  remote/stdio badges; templ page + nav entry. Render tests.
- [x] 5.3 Install flow for remote entries (`POST /settings/mcp-registry/install`,
  PRG) calling the admin install and surfacing the required-env message.
  Handler tests (remote install, stdio-only rejected before any write).
- [x] 5.4 Stdio-only entries render connector guidance instead of an Install
  button. Render test.

## 6. Specs + verify gate

- [x] 6.1 `openspec validate add-mcp-registry-install` passes.
- [x] 6.2 `apps/server/pkg/sdk`: `go build ./... && go test ./...`.
- [x] 6.3 `apps/connector.linux`: `go build ./... && go test ./...`.
- [x] 6.4 `apps/web-ui/gateway`: `go build ./... && go test ./... && task lint`
  (`templ generate` first if `.templ` changed).
- [x] 6.5 Mac app: build + tests on `mcj-mini` (xcodebuild, simulator).
- [ ] 6.6 Manual onboarding check: search `chrome-devtools`, install, confirm
  `npx -y chrome-devtools-mcp@<version>` suggestion, add, and verify the server
  connects and serves tools; verify a remote entry installs from the Web UI and
  a stdio-only entry shows connector guidance.
