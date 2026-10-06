# Tasks — add-mcp-registry-catalog

TDD throughout: each implementation task ships with the unit test beside it.
Build/test/lint from `apps/server`.

## 1. Embedded catalog access layer

- [x] 1.1 `catalog.go`: `//go:embed catalog/servers.json`, parse into
  `[]RegistryServer` (reuse existing type, ignore unknown fields), `sync.Once`
  load with a clear parse error (fail fast). `search(query, limit)`:
  case-insensitive substring over name/title/description, name-ranked,
  deterministic file order, limit-capped (limit <= 0 → `defaultSearchLimit`);
  empty query lists all up to limit. `get(name)` exact lookup.
- [x] 1.2 `catalog_test.go`: embedded catalog parses to 28 entries each with a
  name; search matches name/title/description case-insensitively, respects
  limit, ranks name matches first, empty query lists all; get finds an entry
  and returns false for an unknown name.

## 2. Service rewired to the catalog

- [x] 2.1 `service.go`: `SearchRegistry` serves from the catalog, ignores
  `cursor`, returns empty `NextCursor`; `GetRegistryServer` resolves from the
  catalog and returns `ErrRegistryServerNotFound` when absent;
  `InstallFromRegistry` resolves from the catalog by `dto.RegistryName` (no
  `GetServerVersion`). Map entries via the existing `registryServerToDTO`.
- [x] 2.2 `catalog_test.go` (service tests): `SearchRegistry` and
  `GetRegistryServer` serve from the catalog with no HTTP (no client needed);
  `InstallFromRegistry` resolves a catalog entry — unknown name →
  `ErrRegistryServerNotFound`, stdio-only entry → `ErrUnsupportedRegistryTransport`.
- [x] 2.3 Remove dead remote wiring: drop `registryClient`/`registryCache` from
  `Service`, update `NewService` (return error) and `provideService`, remove
  `ErrRegistryUpstream` and the handler's `ErrRegistryUpstream` case. Delete
  `cache.go` + `cache_test.go`. Keep `registry_client.go` +
  `registry_client_test.go` + `provideRegistryClient` as a retained client with
  a comment (future opt-in "full registry" mode).
- [x] 2.4 Update existing tests/helpers that built `Service` with a
  registry client/cache (`registry_args_test.go`,
  `tool_ownership_test.go`, `tool_settings_resolution_test.go`,
  `tool_invoke_test.go`, `internal/testutil/server.go`).

## 3. Catalog refresh tool + curated source list

- [x] 3.1 `catalog/sources.txt`: curated source list (28 GitHub MCP-registry
  slugs, one per line, `#` comments/blank lines allowed) with a header comment
  explaining the edit-then-run-then-PR workflow.
- [x] 3.2 `cmd/refresh-mcp-catalog/main.go`: regenerate the catalog from
  `sources.txt`. Flags `-sources`/`-out`/`-base-url` (defaults rooted at
  `apps/server`). For each slug fetch `{base}/{slug}?format=json`, extract
  `payload.mcpDetailsRoute.server_data.raw_data.server`, drop `_meta`, require a
  non-empty `name`. Fail loudly (non-zero exit) on a 404/missing name; assemble
  the whole catalog in memory and write only on success (atomic temp+rename), so
  a bad slug never overwrites a good catalog. Preserve `sources.txt` order.
- [x] 3.3 `cmd/refresh-mcp-catalog/main_test.go`: sources parsing
  (comments/blanks/whitespace), extraction + `_meta` stripped + `{"servers":[...]}`
  shape + order, and a failing slug → non-zero exit with no output written
  (httptest, `t.Context()`, no real network).

## 4. Specs + verify gate

- [x] 4.1 `openspec validate add-mcp-registry-catalog` passes.
- [x] 4.2 `apps/server`: `go build ./...` passes.
- [x] 4.3 `apps/server`: `go test ./cmd/refresh-mcp-catalog/... ./domain/mcpregistry/...` passes.
- [x] 4.4 `apps/server`: `golangci-lint run ./cmd/refresh-mcp-catalog/... ./domain/mcpregistry/...` clean.
