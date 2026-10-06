## Why

`/settings/mcp-registry` browses the live official registry
(`registry.modelcontextprotocol.io`), which is slow and low-quality. The prior
mitigation (longer timeout + short-lived cache) tolerated the upstream rather
than removing it. The decision is to ship our own curated set, embedded in the
repo, in the same `server.json` schema as the official registry, and serve
browse + install from that snapshot with no network dependency. CI/PR updates
the curated data.

## What Changes

- **Embedded curated catalog** (`apps/server/domain/mcpregistry/catalog/servers.json`):
  a generated `{"servers": [...]}` file in the official registry schema, 28
  entries, embedded via `//go:embed` and parsed once at startup into the
  existing `RegistryServer` type.
- **Curated source list + refresh tool**: `catalog/sources.txt` records the 28
  curated GitHub MCP-registry slugs (one per line, `#` comments). A new
  `cmd/refresh-mcp-catalog` tool regenerates `servers.json` from that list by
  fetching each `{base}/{slug}?format=json` and extracting
  `payload.mcpDetailsRoute.server_data.raw_data.server` (dropping `_meta`),
  writing atomically only after every slug succeeds.
- **Catalog access layer** (`catalog.go`): `search(query, limit)` —
  case-insensitive substring over name/title/description, name-ranked,
  deterministic, limit-capped — and `get(name)` exact lookup. Load is
  `sync.Once`-guarded and fails fast if the embedded file cannot parse.
- **Service rewired to the catalog** (`service.go`):
  `SearchRegistry`/`GetRegistryServer`/`InstallFromRegistry` resolve from the
  catalog, with no HTTP. `SearchRegistry` ignores the cursor and returns no
  next page. `GetRegistryServer`/`InstallFromRegistry` return
  `ErrRegistryServerNotFound` for absent names.
- **Dead remote wiring removed**: `Service` drops the `registryClient` and
  `registryCache` fields; `NewService`/`provideService` no longer take a
  client; `cache.go`/`cache_test.go` deleted; `ErrRegistryUpstream` removed.
  `RegistryClient` and its config are retained (unused by the browse path) for
  a possible future opt-in "full registry" mode.

## Capabilities

### Modified Capabilities

- `mcp-registry`: browse and install are served from an embedded curated
  catalog in the official `server.json` schema, with no network dependency; the
  catalog is curated via PR.

## Impact

- **Server** — `apps/server/domain/mcpregistry`: new `catalog.go` +
  `catalog_test.go` + `catalog/sources.txt`; `service.go`/`module.go`/`handler.go`
  rewired; `cache.go` removed; `registry_client.go` retained.
  `internal/testutil/server.go` updated for the new `NewService` signature.
- **Server** — `apps/server/cmd/refresh-mcp-catalog`: new `main.go` +
  `main_test.go` (stdlib-only tool; no new external dependencies).
- No change to the Web UI, connector, SDK, or CLI surfaces — the admin
  `search`/`get`/`install` route contracts are unchanged.
- No new external dependencies.
