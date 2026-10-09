## Why

`agent-object-type-scoping` (#1706, refs #1695) added per-agent `objectTypes` /
`relationshipTypes` allowlists and enforced them on the read/query/list paths.
Six gaps were found during review and deliberately deferred (recorded under
"Follow-up (not in scope)" in the archived proposal). This change closes the five
that are in scope — the sixth (`memory://schema/relationships` has no agent
context) stays as an accepted inconsistency.

Refs #1695.

## What Changes

- **Writes are scoped.** `entity-create`, `relationship-create`, `entity-update`
  and `relationship-update` reject a create/update whose type is outside the
  relevant allowlist with a clear error that does not surface the excluded type
  name (an update cannot change a type, so it is gated on the object's/relationship's
  current type). Inline relationship creates declared on an entity spec are
  skipped when their relationship type is excluded, instead of writing and
  echoing an out-of-scope relationship. Empty scope still means "all types".
- **`search-knowledge` threads the scope instead of failing closed.** The tool's
  HTTP loopback now carries the caller's `objectTypes` / `relationshipTypes` in
  the `/query` request, and the `/query` handler attaches them to the internal
  graph-query-agent's context. The executor's scope derivation is hardened to
  NARROW an inherited context scope by the definition's own allowlist
  (`mcp.NarrowTypeScope`) rather than replacing it, so a caller can only ever
  narrow the internal run, never widen it — even if the internal agent's
  definition ever gained an allowlist. A scoped agent now gets correctly scoped
  answers instead of an error.
- **`relationship-list` pagination metadata is accurate.** The relationship
  allowlist is applied as a query-level `type IN (...)` filter BEFORE the
  `limit+1`/cursor pagination, so `total` and `has_more` describe the filtered set
  rather than the raw page. An explicit `type` argument outside the allowlist is
  rejected.
- **`graph-traverse` drops edges to excluded-type nodes.** When the object axis
  is restricted, an edge is dropped only when an endpoint resolves to a returned
  node whose type is OUTSIDE the object allowlist, so an allowed relationship
  cannot reference an object the object allowlist removed. An endpoint that does
  not resolve to a returned node (e.g. one on a later page of a paginated
  traversal) has an unknown type and the edge is kept, matching the unscoped
  traverse service.
- **`entity-history` is type-scoped.** A history request for an object whose type
  is outside the object allowlist is rejected with a clear error (the excluded
  type name is not surfaced); allowed types return history unchanged.
- `memory://schema/relationships` remains unscoped (HTTP MCP transport, no agent
  scope) — unchanged, as before.

## Capabilities

### Modified Capabilities

- `agent-object-type-scoping`: the boundary now also covers write paths, version
  history, `graph-traverse` endpoint reference hygiene, `relationship-list`
  pagination metadata, and the `search-knowledge` loopback (no longer
  fail-closed).

## Impact

- `apps/server/domain/mcp/service.go` — write-path gates in
  `executeBatchCreateEntities` / `executeBatchCreateRelationships` /
  `executeUpdateEntity`; object-type lookup helper; `entity-history` gate.
- `apps/server/domain/mcp/graph_tools.go` — `executeListRelationships` filters
  before pagination; `executeUpdateRelationship` gate + relationship-type lookup.
- `apps/server/domain/mcp/graph_scope.go` — `filterTraverseResponseByScope` drops
  edges whose endpoint resolves to a returned node outside the object allowlist
  (unknown/off-page endpoints are kept).
- `apps/server/domain/mcp/query_tools.go` — `search-knowledge` threads the scope
  through the `/query` loopback instead of failing closed.
- `apps/server/domain/graph/repository.go` — `RelationshipListParams.Types`
  applied by `CountRelationships` / `ListRelationships` before pagination.
- `apps/server/domain/chat/handler.go` — `/query` accepts `object_types` /
  `relationship_types` and attaches them to the internal run context.
- No database migration, no new dependency, no breaking API change: an absent or
  empty allowlist preserves today's unrestricted behaviour on every surface.
