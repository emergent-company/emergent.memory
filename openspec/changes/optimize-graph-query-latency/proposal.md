## Why

The web-ui "Ask the graph" query (dev) takes 55–68s. Tracing showed the internal graph-query-agent's MCP tool `entity-type-list` takes 26–34s and `search-hybrid` 0.5–35s. The `entity-type-list` tool unconditionally runs an expensive relationship-type aggregation (a three-way join across the whole relationship graph), and `search-hybrid` runs the relationship-vector search leg even for simple entity/text lookups that never need it.

## What Changes

- Make the `entity-type-list` relationship-type aggregation opt-in behind `include_relationships` (default false), and fix the relationship SQL so it is project-scoped and can use the partial index `idx_graph_relationships_head_main`.
- Make `search-hybrid`'s relationship (triple) candidate leg opt-in behind `include_relationships` (default false), plumbed into unified search's new `IncludeRelationships` field.
- Bound the unified-search query-embedding provider call with a timeout so a slow/hung provider cannot stall the whole search.
- Update the graph-query-agent system prompt to stop calling `entity-type-list` for simple lookups and to use `include_relationships` only for relationship-centric questions.

## Capabilities

### Modified Capabilities

- `mcp-tool-results`: `entity-type-list` and `search-hybrid` gain an `include_relationships` input that gates expensive relationship work.
- `search`: unified search gains an `IncludeRelationships` request field that skips the relationship-vector leg when false.

## Impact

- `apps/server/domain/mcp/service.go`, `graph_tools.go`: tool schemas, `executeListEntityTypes`, `executeHybridSearch`, `readEntityTypesResource`.
- `apps/server/domain/search/dto.go`, `service.go`: `IncludeRelationships` field, relationship-leg skip, embedding timeout.
- `apps/server/domain/agents/repository.go`: graph-query-agent system prompt.
