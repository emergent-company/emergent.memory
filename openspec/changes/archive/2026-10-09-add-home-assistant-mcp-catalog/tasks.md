# Tasks — add-home-assistant-mcp-catalog

## 1. Refresh tool: two source kinds

- [x] 1.1 Add official-registry consts (`defaultRegistryBaseURL`,
  `registryAPIVersion`, `officialPrefix`) and the `-registry-base-url` flag.
- [x] 1.2 Dispatch per source line: `official:` → official fetch, else GitHub.
- [x] 1.3 Implement official fetch (`GET
  {registry-base}/v0.1/servers/{name}/versions/latest`, `%2F`-escaped name),
  extracting `{"server": ...}` and stripping `_meta`.
- [x] 1.4 Preserve sources.txt order; assemble in memory; write nothing on
  failure (existing atomic temp+rename).

## 2. Tests

- [x] 2.1 `parseSources` keeps an `official:` line verbatim.
- [x] 2.2 Official extraction strips `_meta` and requires a name.
- [x] 2.3 Mixed end-to-end run (GitHub base + registry base httptest servers)
  asserts order and count.
- [x] 2.4 Failing `official:` source → non-zero exit and no output file written.

## 3. Catalog entry

- [x] 3.1 Update `sources.txt` header to document both source kinds.
- [x] 3.2 Append `official:io.github.homeassistant-ai/ha-mcp`.
- [x] 3.3 Regenerate `servers.json` (29 entries, last = HA MCP).
- [x] 3.4 Bump embedded-catalog count assertion 28 → 29.

## 4. Spec + verify gate

- [x] 4.1 `openspec validate add-home-assistant-mcp-catalog` passes.
- [x] 4.2 `cd apps/server && go build ./...`.
- [x] 4.3 `cd apps/server && go test ./cmd/refresh-mcp-catalog/... ./domain/mcpregistry/...`.
- [x] 4.4 `cd apps/server && golangci-lint run ./cmd/refresh-mcp-catalog/... ./domain/mcpregistry/...`.
