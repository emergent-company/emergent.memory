## Why

Relationship triplet embeddings are generated and indexed but underutilized in search. Three of five fusion strategies silently drop relationship results, each unified search makes three redundant embedding API calls (costing ~200-400ms extra latency and 66% wasted Vertex AI spend), and the IVFFlat vector indexes run at minimum recall (probes=1). Graph expansion follows all edges equally with no query awareness. These are concrete gaps between the infrastructure we've built and the value it delivers.

## What Changes

- **Deduplicate query embedding**: Unified search embeds the query once and passes the vector to all three parallel search goroutines, eliminating two redundant Vertex AI API calls per search
- **Increase IVFFlat recall**: Set `ivfflat.probes` to 10 (from default 1) for vector search queries across all three indexes (objects, chunks, relationships)
- **Fix fusion strategy gaps**: Include relationship results in `interleave`, `graph_first`, and `text_first` fusion strategies (currently only `weighted` and `rrf` use them)
- **Add independent relationship weight**: New `relationshipWeight` parameter for the `weighted` fusion strategy (defaults to `graphWeight` for backward compatibility)
- **Query-aware graph expansion**: When expansion is initiated from a search context, use relationship embeddings to prioritize which edges to traverse via a similarity-scored priority queue instead of unranked BFS

## Capabilities

### New Capabilities

- `query-aware-expansion`: Edge-ranked graph expansion using relationship embeddings and query context to prioritize traversal paths by semantic relevance

### Modified Capabilities

- `unified-search`: Deduplicate embedding calls, include relationship results in all fusion strategies, add `relationshipWeight` parameter
- `relationship-search`: Increase IVFFlat probes for better recall, make relationship results participate in all fusion modes
- `graph-search`: Increase IVFFlat probes, support query-aware expansion as an optional mode

## Impact

- **Backend**: `search/service.go` (unified search orchestrator), `search/repository.go` (relationship queries), `graph/repository.go` (ExpandGraph, vector queries), `graph/service.go` (hybrid search fusion), `graph/dto.go` (new parameters)
- **MCP**: `mcp/service.go` (MCP tool wrappers for search and expansion)
- **APIs**: New optional `relationshipWeight` field on unified search request; new optional `queryContext` field on expansion endpoints. All additive, no breaking changes.
- **Performance**: Net positive — faster search (~200ms saved), better recall, lower Vertex AI costs
- **Database**: No schema changes. IVFFlat probes is a runtime session setting.
