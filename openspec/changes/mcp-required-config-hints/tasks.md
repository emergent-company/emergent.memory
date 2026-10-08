## 1. Server — carry the registry guidance

- [x] 1.1 `mcpregistry/entity.go`: add `Description` + `ValueHint` (`omitempty`) to `RequiredConfigKey` and refresh its doc comment
- [x] 1.2 `mcpregistry/service.go`: `collectRegistryRequiredConfig` copies `RegistryHeader.Description`/`Value`, `RegistryEnvVar.Description` (trimmed)
- [x] 1.3 `apps/server/migrations/00229_update_required_config_comment.sql`: refresh the `required_config` column comment for the additive shape
- [x] 1.4 Server unit tests: description carried for header + env; header `value` becomes `ValueHint`

## 2. Web UI client

- [x] 2.1 `extras.go`: `MCPRequiredConfigKey` gains `Description` + `ValueHint` (wire fields)

## 3. Web UI rendering

- [x] 3.1 `mcp_servers.go`: `mcpKVField` gains `Hint`/`Placeholder`; `mcpKVFieldsWithSecrets` takes the kind's `[]MCPRequiredConfigKey` and attaches hint/placeholder to matching rows
- [x] 3.2 `mcp_servers.templ`: required rows render the registry description (`data-mcp-kv-hint`) and use `ValueHint` as the value placeholder (never prefilled)
- [x] 3.3 Gateway unit tests: description hint renders; value template becomes the placeholder, not a prefilled value

## 4. Verify

- [x] 4.1 `templ generate` (web-ui; generated output is gitignored)
- [x] 4.2 `go build ./...` in `apps/server` and `apps/web-ui/gateway`
- [x] 4.3 `go test ./...` in both modules
- [x] 4.4 `golangci-lint run` clean in both modules
- [x] 4.5 `openspec validate mcp-required-config-hints --strict`
- [ ] 4.6 Playwright e2e around the registry install/configure flow — deferred (needs a live memory backend + registry; the existing `mcp-servers-create-ui` spec dials `E2E_MCP_EXAMPLE_URL`). Covered by gateway unit tests.
- [ ] 4.7 Server restart + browser check of the GitHub configure form — deferred to the review environment.
