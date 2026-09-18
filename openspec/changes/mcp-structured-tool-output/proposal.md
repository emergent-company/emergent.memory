## Why

Memory's MCP tools already return a uniform, machine-classifiable envelope (`{ok, error, data, meta}`) serialized into a single human-readable text `content` block. But that envelope is only exposed as a JSON string inside text — a programmatic consumer must parse prose to reach it. MCP 2025-06-18 adds `tools/list` → `Tool.outputSchema` and `tools/call` → `CallToolResult.structuredContent`, letting servers advertise a schema for and return a first-class JSON object result. Adopting these two fields gives deterministic, schema-validated programmatic consumers without breaking the existing text contract (issue #586).

## What Changes

- Add `OutputSchema *InputSchema` to `ToolDefinition` and `StructuredContent map[string]any` to `ToolResult`.
- Add a shared `envelopeOutputSchema()` helper describing the envelope shape (`{type:"object", properties:{ok,error,data,meta}, required:[ok,data]}`).
- `envelopeResult` now sets `StructuredContent` to the same object it marshals into the text block; `wrapResult`/`wrapResultCompact` set `StructuredContent` only when the marshalled payload is a JSON object (arrays/primitives stay text-only, per spec).
- Declare `outputSchema: envelopeOutputSchema()` on every envelope-producing tool (`entity-create`, `entity-update`, `entity-delete`, `relationship-create`, `relationship-delete`, `search-hybrid`, `search-semantic`, `entity-query`, `remember`, `forget`). `wrapResult`/prose tools keep `OutputSchema` nil.
- Forward `outputSchema` and `structuredContent` through the proxy and relay paths so proxied/relayed tools expose them.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `mcp-tool-results`: the uniform-envelope contract gains a structured-output layer — envelope tools declare an `outputSchema`, and the server returns `structuredContent` mirroring the text block, while the text `content` block remains and `isError` stays reserved for call failure.

## Impact

- Server (Go): `domain/mcp/entity.go`, `domain/mcp/envelope.go`, `domain/mcp/response_contract.go`, `domain/mcp/service.go`, `domain/mcpregistry/proxy.go`, `domain/mcpregistry/entity.go`, `domain/mcpregistry/service.go`, `domain/agents/toolpool.go`.
- Tests: `domain/mcp/structured_content_test.go` plus existing envelope/registry tests.
- Compatibility: additive and non-breaking. Text `content` JSON is byte-identical; new fields (`outputSchema`, `structuredContent`) are additive. No tool names, input schemas, or payload field names change.
