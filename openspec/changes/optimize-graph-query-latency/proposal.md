## Why

The web-ui "Ask the graph" query (dev) takes 55–68s. Tracing showed the internal graph-query-agent's MCP tool `entity-type-list` takes 26–34s and `search-hybrid` 0.5–35s. The `entity-type-list` tool unconditionally runs an expensive relationship-type aggregation (a three-way join across the whole relationship graph), and `search-hybrid` runs the relationship-vector search leg even though MCP never surfaces its candidates.

## What Changes

- Make the `entity-type-list` relationship-type aggregation opt-in behind `include_relationships` (default false). This is the actual latency fix: the aggregation is skipped entirely on the default path. The relationship SQL is additionally made project-scoped and head-only (`gr.supersedes_id IS NULL`, `src/dst.project_id`) for correctness; note that on the dev dataset the planner still chooses a parallel sequential scan for this three-way grouping join, so the SQL change is not what removes the cost.
- Make the MCP `search-hybrid` path always skip the relationship-vector search leg (set `includeRelationships: false`), since `mapUnifiedToSearchResponse` only maps graph/text items and the leg's candidates are never surfaced to MCP clients.
- Add an `IncludeRelationships` field to unified search so callers can skip the relationship leg; bound the query-embedding provider calls (shared and per-leg) with a timeout so a slow/hung provider cannot stall the search.
- Update the graph-query-agent system prompt to stop calling `entity-type-list` for simple lookups and to use `include_relationships` only for relationship-type questions.

## Capabilities

### Modified Capabilities

- `mcp-tool-results`: `entity-type-list` gains an `include_relationships` input that gates the expensive relationship-type aggregation; `search-hybrid` no longer runs (or exposes) the relationship candidate leg.
- `search`: unified search gains an `IncludeRelationships` request field that skips the relationship-vector leg when false; the MCP `search-hybrid` path always sets it false.

## Impact

- `apps/server/domain/mcp/service.go`, `graph_tools.go`: tool schemas, `executeListEntityTypes`, `executeHybridSearch`, `readEntityTypesResource`.
- `apps/server/domain/search/dto.go`, `service.go`: `IncludeRelationships` field, relationship-leg skip, embedding timeout.
- `apps/server/domain/agents/repository.go`: graph-query-agent system prompt.
