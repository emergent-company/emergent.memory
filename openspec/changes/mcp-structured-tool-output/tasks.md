## 1. Core types

- [x] 1.1 Add `OutputSchema *InputSchema` (`json:"outputSchema,omitempty"`) to `ToolDefinition` in `domain/mcp/entity.go`.
- [x] 1.2 Add `StructuredContent map[string]any` (`json:"structuredContent,omitempty"`) to `ToolResult` in `domain/mcp/entity.go`.
- [x] 1.3 Add `envelopeOutputSchema()` returning the shared envelope output schema in `domain/mcp/envelope.go`.

## 2. Populate structured content

- [x] 2.1 `envelopeResult` sets `StructuredContent` to the same `env` map marshalled into the text block.
- [x] 2.2 `envelopeSearchResponse` unchanged (delegates to `envelopeResult`).
- [x] 2.3 `wrapResult` / `wrapResultCompact` set `StructuredContent` only when the marshalled payload is a JSON object (via `structuredContentFromJSON`), leaving text byte-identical.

## 3. Declare output schema on envelope tools

- [x] 3.1 Declare `OutputSchema: envelopeOutputSchema()` on all ten envelope tools (`entity-create`, `entity-update`, `entity-delete`, `relationship-create`, `relationship-delete`, `search-hybrid`, `search-semantic`, `entity-query`, `remember`, `forget`).
- [x] 3.2 Leave `wrapResult`/prose tools with nil `OutputSchema`.

## 4. Transports

- [x] 4.1 Confirm `streamable_http_handler.go`, `sse_handler.go`, `handler.go`, and `agent_endpoint_handler.go` marshal the `ToolDefinition`/`ToolResult` structs directly (no hand-built maps), so the new fields serialize automatically.

## 5. Proxy and relay paths

- [x] 5.1 `convertCallToolResult` (mcpregistry/proxy.go) copies `StructuredContent` (normalized to `map[string]any` when it is a JSON object).
- [x] 5.2 Add `convertToolOutputSchema` and forward `OutputSchema` in `DiscoverTools`/`InspectServer` (with `OutputSchema` fields on `DiscoveredTool` and `InspectToolDTO`).
- [x] 5.3 `relaySessionTools` (mcp/service.go) carries `outputSchema` into relayed tool definitions.
- [x] 5.4 `extractRelayToolDefs` (agents/toolpool.go) carries `outputSchema` into pooled relay tool definitions.
- [x] 5.5 Verify `convertToolResult` (agents/toolpool.go) still compiles and preserves behavior (it ignores `StructuredContent`).

## 6. Tests

- [x] 6.1 `envelopeResult` sets `StructuredContent` identical to the text JSON.
- [x] 6.2 A tool declared with `envelopeOutputSchema()` serializes an `outputSchema` field in `tools/list`.
- [x] 6.3 A `tools/call` result serializes `structuredContent` alongside `content`.
- [x] 6.4 `wrapResult`/`wrapResultCompact` set structured content only for object payloads.

## 7. Verification

- [x] 7.1 `go build ./...` from `apps/server`.
- [x] 7.2 `go test ./domain/mcp/... ./domain/mcpregistry/... ./domain/agents/...`.
- [x] 7.3 `golangci-lint run ./domain/mcp/... ./domain/mcpregistry/...`.
- [x] 7.4 `openspec validate mcp-structured-tool-output --strict`.
