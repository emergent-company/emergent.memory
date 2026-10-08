## 1. Sanitize the templated non-`pageError` fields (`web-ui-degraded-backend-state`)

- [x] 1.1 Inventory every templated non-`pageError` error field (`.PendingErr`, `.ToolsErr`, `.ChatsErr`, `.StatusErr`/`.CoverageErr`, `.GeneralErr`, `.SaveErr`, `.MCPActionErr`, providers `d.Err`) and its assignment site.
- [x] 1.2 Add `redactBackendError` to `memory.go` for surfaces that mix local copy with backend failures.
- [x] 1.3 Redact `.PendingErr`, `.ToolsErr`, `.ChatsErr` at their assignment sites.
- [x] 1.4 Redact `.StatusErr`/`.CoverageErr` (embeddings) at their assignment sites.
- [x] 1.5 Redact `.GeneralErr` (MCP server create/update, MCP share create/update) at their assignment sites.
- [x] 1.6 Redact providers `d.Err` at all three `loadProviderPanel` assignment sites.
- [x] 1.7 Redact `.SaveErr` via `redactBackendError`, preserving local validation copy.
- [x] 1.8 Redact `.MCPActionErr` via `agentMCPErrText`, preserving 4xx/sentinel copy.
- [x] 1.9 Confirm no double-wrapping of already-sanitized pass-through fields.

## 2. Tests

- [x] 2.1 Add `TestTemplatedErrorFieldsRedactRawBackendErrors` (table-driven): representative affected pages render `memoryUnavailableMessage` and none of `dial tcp`, the IP:port, `memory 5`, `service_unavailable`, `Token validation`.
- [x] 2.2 Add a focused `redactBackendError` unit test: backend/transport sanitized, local validation error preserved.
- [x] 2.3 Mutation check: reverting one sanitized assignment to the raw error makes the new test fail.

## 3. Verification

- [x] 3.1 `go build ./...`, `go vet ./...`, `go test ./...` from `apps/web-ui/gateway`.
- [x] 3.2 `golangci-lint run ./...` (0 issues).
- [x] 3.3 `openspec validate web-ui-templated-error-fields --strict`.
- [x] 3.4 Grep the templates: no raw backend `.Error()` remains on the listed fields.
