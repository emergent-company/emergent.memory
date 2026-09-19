## Why

PR #588 adopted MCP 2025-06-18 `structuredContent` + `outputSchema` but only declared `outputSchema` on the ten `envelopeResult`-based tools (plus the four agent-endpoint session tools). Of ~109 static tool definitions, ~95 still expose no declared output contract, so a programmatic consumer cannot discover a result shape from `tools/list` for most of the surface. Issue #586 asks for the whole surface to carry a declared, truthful contract.

## What Changes

- Add `additionalProperties` to `InputSchema` (reused as `outputSchema`) so a permissive root-object schema is expressible. Without it, an object schema with empty `properties` rejects every key.
- Add two shared schema helpers alongside `envelopeOutputSchema()`:
  - `objectOutputSchema()` — `{ "type": "object", "additionalProperties": true }`, the minimum truthful shape for tools whose top-level keys are dynamic or owned by another domain.
  - `typedObjectOutputSchema(props, required)` — a known-key object schema that still permits additional keys.
- Export `StructuredContentFromJSON` and make `wrapResult`/`wrapResultCompact` use it, so JSON results always populate `structuredContent` as a root object: object → itself, array → `{"results": [...]}`, primitive → `{"value": ...}`.
- Declare `outputSchema` on every static MCP tool: `envelopeOutputSchema()` for envelope tools, `objectOutputSchema()` for the rest, `typedObjectOutputSchema` where a stable top-level key is known (`queue-reextraction` → `job_id`; `session-todo-list` → `todos`).
- Reroute raw `json.Marshal`-built results through `wrapResultCompact` so they emit `structuredContent`.
- Make the blueprints and agents domain result helpers emit `structuredContent` (their MCP tools delegate result construction there).
- Keep an explicit prose-only allowlist (raw text/markdown results, no object root) that are exempt from declaring `outputSchema`.
- Replace the partial coverage test with one that asserts **every** static tool declares an object-root `outputSchema` unless on the prose-only allowlist.

## Capabilities

### Modified Capabilities

- `mcp-tool-results`: adds requirements for declared `outputSchema` coverage and truthful `structuredContent` emission across the full tool surface, while preserving the existing text `content` contract.

## Impact

- **`apps/server/domain/mcp/`**: `entity.go` (schema field), `envelope.go` (helpers + allowlist), `response_contract.go` (exported wrapper), `service.go` + all `*_tools.go` (declared schemas, RAWOBJ reroute), `structured_content_test.go` (coverage guard).
- **`apps/server/domain/blueprints/`** and **`apps/server/domain/agents/`**: tool-result helpers now set `StructuredContent`.
- **No breaking changes** — `content[].text` remains the authoritative human payload and is unchanged; `outputSchema`/`structuredContent` are additive and ignored by pre-2025-06-18 clients.
- **Issue**: closes #586.
