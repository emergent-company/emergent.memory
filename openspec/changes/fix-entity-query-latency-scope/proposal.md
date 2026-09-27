## Why

`entity-query` (and the `query entities` store call behind it) takes 80–150 s for a
single filtered property query on a content-heavy type, and returns wrong-scope
results. On dev, project `3bb99dc3-…` (Norwegian law graph), a filter
`{"type_name":"LegalParagraph","filters":{"chapter_id":"kapittel-2-kapittel-1"},
"field_strategy":"full","limit":30}` matched 697 rows spanning **multiple
regulations**, and four parallel calls took ~83 s / ~116 s / two `context canceled`
at ~150 s (kb.agent_run_tool_calls run `613e86c3-…`).

Two independent defects:

1. **Latency.** `filters` compile to `properties->>'key' = 'value'` on
   `kb.graph_objects`. There is no index on `properties`, so the pagination
   `COUNT(*)` plans as a full Parallel Seq Scan and detoasts the wide properties
   JSONB (~14 KB/statute row) for every candidate; the agent turn stalls long enough
   for the browser SSE connection to drop and the run to die (see #1149).
2. **Wrong scope.** `chapter_id` is not unique to one entity identity — every law and
   regulation has a chapter 2 — so filtering by it alone returns paragraphs from
   unrelated documents. The entity key (`<law_ref_id>#<section_id>` for
   `LegalParagraph`) is the identity; the filter never scopes to it.

## What Changes

- **Index + query shape.** Add a composite partial GIN index
  `(project_id, type, properties jsonb_path_ops)` on head-main rows
  (`deleted_at IS NULL AND supersedes_id IS NULL AND branch_id IS NULL`), using
  `btree_gin` so project and type are index conditions (a property value common
  across projects would otherwise hand the GIN alone a large candidate set).
  Rewrite the filter from `properties->>'k' = 'v'` string interpolation to a bound
  JSONB containment predicate `properties @> $jsonb`, which is what the GIN serves
  and which also removes the interpolation. This is a **recorded behaviour change**:
  containment is JSON-type-exact, so `{"k":"5"}` matches a string `"5"` but no
  longer a numeric `5` (the old `->>'k' = '5'` text comparison matched both).
- **Bounded calls.** Hard per-call deadline (`MCP_ENTITY_QUERY_TIMEOUT`, default
  30 s) created at the tool entry so it covers every path — branch resolution, the
  `ids[]` fast-path, the type/pagination queries, and relationship enrichment —
  returning an explicit `timed out after …` error instead of a `context canceled`
  after minutes; and a cap on the effective `limit` when `field_strategy="full"`
  (`MCP_ENTITY_QUERY_FULL_MAX_LIMIT`, default 25) with a caller-visible warning.
- **Identity scoping.** Add an optional `key_prefix` parameter that restricts
  results to entities whose canonical key starts with the prefix, so a non-unique
  property filter (e.g. `chapter_id`) can be scoped to one document
  (`key_prefix: "lov/1997-06-13-44#"`). Generic across types: the key is the
  identity. `key_prefix` combined with `ids` is **rejected** fail-closed (an
  explicit id list already identifies entities exactly), rather than silently
  ignored.

## Capabilities

### New Capabilities

<!-- None. -->

### Modified Capabilities

- `mcp-tool-results`: entity-query property filters are indexed containment
  predicates; entity-query is bounded by a per-call timeout and a full-strategy
  limit cap; entity-query supports key-prefix identity scoping.

## Impact

- **Migration** `apps/server/migrations/00197_…` (numbered above origin/main's
  max 00194 and above open PR #1163's 00195/00196): `CREATE EXTENSION IF NOT EXISTS
  btree_gin` + `CREATE INDEX CONCURRENTLY` (drop-if-exists guard, `NO TRANSACTION`).
- **Server** `apps/server/domain/mcp/service.go`: filter build, `key_prefix`,
  timeout, full-limit cap, tool schema/description.
- **Config** `apps/server/internal/config/config.go`: new `MCPConfig`.
- **Tests** `apps/server/domain/mcp/entity_query_filter_test.go` (DB-backed):
  scope leak, key-prefix scoping, full-limit bound, timeout.
- **Deferred (product decision).** Auto-scoping `chapter_id` by the parent
  document key (rather than requiring callers to pass `key_prefix`/`law_ref_id`)
  changes the filter contract and is not implemented; see the PR for options.
