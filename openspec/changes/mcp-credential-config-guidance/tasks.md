## 1. Server — remote credentials + provenance

- [x] 1.1 `mcpregistry/entity.go`: `RegistryRemoteDTO` carries `Headers` + `EnvironmentVariables`; `MCPServer`/`MCPServerDTO`/`CreateMCPServerDTO` gain registry provenance
- [x] 1.2 `mcpregistry/service.go`: `registryServerToDTO` populates per-remote credentials; `InstallFromRegistry` records registry name + repository URL
- [x] 1.3 Migration `00230_add_registry_provenance_to_mcp_servers.sql`
- [x] 1.4 Server unit tests: DTO exposes remote credentials; DB suite asserts guidance + provenance persist

## 2. Server — auth-rejection classification

- [x] 2.1 `mcpregistry/errors.go`: `ErrMCPAuthRejected{Status}` with an actionable message
- [x] 2.2 `mcpregistry/outbound.go`: `authStatusTransport` maps 401/403 to the typed error
- [x] 2.3 Server unit test: 401/403 classify; other statuses pass through

## 3. Web UI — gateway

- [x] 3.1 `mcp_registry_client.go`/`extras.go`: remote headers + env wire types; `MCPServer` provenance fields
- [x] 3.2 `mcp_registry.templ`: registry listing renders the call to action inline (right-hand column), no credential preview
- [x] 3.3 `mcp_servers.templ`: configure banner lists missing keys with descriptions + provider docs link
- [x] 3.4 `mcp_servers.templ`: required `Authorization` renders scheme + token; hidden `.auth`/`.scheme` keep arrays aligned (incl. JS-added rows)
- [x] 3.5 `mcp_servers.go`: auth helpers (`mcpAuthHeaderValue`, `mcpAuthSchemeFromHint`), banner details, docs link
- [x] 3.6 Gateway unit tests: assembly, alignment, docs link, configure render

## 4. Verify

- [x] 4.1 `templ generate` (web-ui)
- [x] 4.2 `go build ./...` in `apps/server` and `apps/web-ui/gateway`
- [x] 4.3 `go test ./...` in both modules
- [x] 4.4 `golangci-lint run` clean in both modules
- [x] 4.5 `openspec validate mcp-credential-config-guidance --strict`
- [ ] 4.6 Playwright e2e + browser check — deferred (needs a live backend + registry)
