## Why

`entity-query` scopes results with an optional `key_prefix`. The predicate was
`starts_with(go.key, ?)` — a function call, so the planner could not turn it into
a btree range. On a real corpus (dev: ~95k `LegalParagraph` head rows in one
project, 310 MB heap) every `key_prefix` call planned as a **Parallel Seq Scan of
the whole `kb.graph_objects` heap**: ~20.5 s per call, constant regardless of how
many rows the `limit` actually returned. Five such calls consumed ~93 s of a
5.5-minute agent run, each at ~68 % of the tool's own 30 s deadline (#1191).

The observed cost was **not** per-row JSONB detoast — a hermetic DB with 120k
rows reproduced the same full-heap scan at 0.04 s/row-equivalent and the same
query with an indexable range ran in 0.06 ms execution. The original report
attributed the cost to `field_strategy="full"` returning large `properties`
rows, but the projected `content` on the affected corpus averaged 674 bytes; the
constant 20.5 s was the scan, not the projection.

## What Changes

- `entity-query`'s `key_prefix` predicate is rewritten from `starts_with(go.key,
  ?)` to an explicit bytewise range `go.key COLLATE "C" >= ? AND go.key COLLATE
  "C" < ?`. The upper bound is the prefix with its last **rune** incremented to
  the next valid code point — a valid-UTF-8 string strictly greater than every
  extension of the prefix. Incrementing the final byte (an earlier draft of this
  change) emits invalid UTF-8 for prefixes ending in `0x7F` or a `0xBF`
  continuation byte: a text-protocol client then rejects the bind with SQLSTATE
  22021, and a binary-format client such as pgx silently compares the invalid
  bytes and returns the wrong rows. Bytewise `COLLATE "C"` is required because
  the default collation is not bytewise and would over-exclude
  separator-terminated prefixes.
- Migration `00198_add_graph_objects_project_type_key_c.sql` adds the partial
  bytewise index `idx_graph_objects_project_type_key_c (project_id, type, key
  COLLATE "C")` on head-main rows, which serves that range. No query-shape or
  response change: the same entities are returned.
- `entity-query` surfaces a relationship-enrichment failure instead of
  swallowing it, so a deadline exhaustion returns the explicit timeout error the
  `mcp-tool-results` spec already requires rather than `ok:true` with every
  entity's relationships silently dropped (#1187 class).
- The `limit` input schema now states the lower effective cap applied under
  `field_strategy="full"`, so the advertised `Maximum: 200` no longer contradicts
  the runtime clamp (#1188).

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `mcp-tool-results`: entity-query key-prefix scoping is index-backed; the
  enforced full-strategy cap is stated in the input schema; relationship
  enrichment failures are surfaced.

## Impact

- `apps/server/domain/mcp/service.go` — `keyPrefixRangeClause` replaces the
  `starts_with` clause; the `limit` schema description interpolates the
  effective cap; enrichment errors are no longer discarded.
- `apps/server/migrations/00198_add_graph_objects_project_type_key_c.sql` — new
  partial `COLLATE "C"` index.
- `apps/server/domain/mcp/entity_query_key_prefix_test.go` — clause-bound and
  plan/index regression tests, plus the schema-cap test.
- No response shape, pagination, filter, ordering, `fields[]` or scope-key
  semantics change.
- No `MCP_ENTITY_QUERY_TIMEOUT` default change; the deadline is kept (see the
  proposal section in the PR) because the scan removal makes the worst case fit
  comfortably, and timeout expiry already returns an explicit error.
