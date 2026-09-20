## Why

Agent chat runs died with `context canceled` because the search MCP tools (`search-hybrid`, `entity-search`, …) returned unbounded payloads into the model context — one `search-hybrid` result was 756 KB and an `entity-search` result 175 KB. The model chose `field_strategy: "full"`, which emits the full `properties` blob per result; for `LegalParagraph` entities that is the full paragraph text. There is no size cap: `limit` caps result count, not bytes, and `slimEntity`/`propertiesForStrategy` emit `properties` untruncated under `full`. Oversized context → slow first search (~5 min) → run exceeds connection tolerance → `context canceled`.

Ref: issue #673.

## What Changes

- Add a server-side per-property-value cap in the MCP response contract: any string property value longer than 4000 characters is truncated (recursively into nested maps/arrays) with an explicit truncation marker.
- Apply the cap to every read/search/list path that emits entity or relationship `properties` — the slim contract (`slimEntity`, `slimRelationship`, search/similar/traverse) and the raw `Entity`/`EdgeInfo`/`ConnectedEntity` emissions in `entity-query`, `entity-search`, and `entity-edges-get`.

## Capabilities

### Modified Capabilities

- `mcp-tool-results`: bound emitted property value size for read tools.

## Impact

### Code Changes
- `apps/server/domain/mcp/response_contract.go`: add `maxPropertyValueChars`, `truncateProperties`, `truncatePropertyValue`, `truncateString`; apply in `propertiesForStrategy` and `slimRelationship`.
- `apps/server/domain/mcp/service.go`: apply `truncateProperties` at the six raw `Properties` emission points (`executeQueryEntities`, `executeQueryEntitiesByIDs`, `executeSearchEntities`, `executeGetEntityEdges` incoming + outgoing, `getEntityBasicInfo`).

### Behavior Changes
- Read/search/list tool results SHALL truncate oversized string property values with a marker; values under the cap are unchanged. No schema or API surface change.

### Non-Goals
- Batch-create tools that echo user-supplied input back are not covered by this change (input is already bounded by the request size).

### Testing Requirements
- Unit tests for truncation (long string, nested map/array, non-string passthrough, marker, no input mutation, under-cap unchanged).
