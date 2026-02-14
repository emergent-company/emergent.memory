## Context

Unified search (`search/service.go:Search`) orchestrates three parallel goroutines — graph, text, and relationship search — then fuses their results via one of five strategies. Today each goroutine independently calls `embeddings.EmbedQuery()`, producing three identical Vertex AI round-trips (~200ms each). Three of five fusion strategies (`interleave`, `graph_first`, `text_first`) accept only graph+text params and silently discard relationship results. The `weighted` strategy uses `graphWeight` for relationship scores (no independent control). All IVFFlat vector indexes run at default `probes=1` (scan 1 of ~100 lists). `ExpandGraph` performs standard BFS with no awareness of the query that initiated the search.

The existing code is well-structured for these changes: the three goroutines already receive a shared `req` struct, fusion functions have a clean switch dispatch, and `ExpandGraph` has a clear per-level loop with explicit edge collection. No schema migration is required — IVFFlat probes is a session-level PostgreSQL setting.

## Goals / Non-Goals

**Goals:**

- Eliminate 2 of 3 redundant embedding API calls per unified search (~200-400ms latency, ~66% cost reduction)
- Make all 5 fusion strategies include relationship results
- Add independent `relationshipWeight` for weighted fusion
- Increase IVFFlat recall by setting `probes=10` on vector search queries
- Add optional query-aware edge prioritization to `ExpandGraph`
- All changes are backward-compatible (additive parameters, same defaults)

**Non-Goals:**

- Changing the embedding model or dimensionality
- Modifying the extraction pipeline or how triplet embeddings are generated
- Schema migration or new database indexes
- Adding new API endpoints (only modifying existing ones)
- Changing the BFS depth/node/edge limits or direction semantics
- Implementing ontology auto-discovery or template pack changes

## Decisions

### D1: Embed once in `Search()`, pass vector to goroutines

**Decision:** Move the single `EmbedQuery()` call to the top of `Search()` (before goroutine launch) and pass the resulting `[]float32` vector to all three `execute*` functions via a new field on the existing request or an explicit parameter.

**Approach:** Add a `queryVector []float32` parameter to `executeGraphSearch`, `executeTextSearch`, and `executeRelationshipSearch`. Each function uses the pre-computed vector if non-nil; otherwise falls back to calling `EmbedQuery` itself (for standalone invocation paths like `chat/handler.go` that call `graphService.HybridSearch` directly — those paths are unaffected).

**Alternatives considered:**

- _Cache in embeddings.Service:_ Would require a concurrency-safe cache keyed by query string. Over-engineered for this case since the duplicate calls are within a single function scope, not across services. Would also mask the cost — callers wouldn't know if they're paying for an API call or not.
- _Add vector field to UnifiedSearchRequest DTO:_ Would expose internal implementation detail to API clients. The vector is an internal optimization, not a client concern.

**Why this approach:** Minimal change surface. The orchestrator already has the query string, and the three functions already accept the request struct. Adding a parameter keeps the change local to `search/service.go` with no cross-package ripple.

### D2: Fix fusion strategies by adding `relationshipResults` parameter to all fuse functions

**Decision:** Change the signature of `fuseInterleave`, `fuseGraphFirst`, and `fuseTextFirst` to accept `relationshipResults []*RelationshipSearchResult` as a third parameter, matching the existing signature of `fuseWeighted` and `fuseRRF`.

**Approach:**

- `fuseInterleave`: Three-way round-robin — graph, text, relationship, graph, text, relationship, ... When one source is exhausted, continue alternating between the remaining two.
- `fuseGraphFirst`: graph → relationship → text (relationships are conceptually graph-adjacent)
- `fuseTextFirst`: text → relationship → graph (mirror ordering)

**Alternatives considered:**

- _Append relationships to graph results before fusion:_ Would conflate relationship items with graph objects, losing the `ItemTypeRelationship` type discriminator. Clients that filter by type would break.
- _Separate relationship fusion pass:_ Unnecessary complexity. The existing pattern of a single fusion function per strategy is clean.

**Why this approach:** Consistent function signatures across all strategies. The ordering choices (relationship between graph and text) reflect that relationships are structurally part of the knowledge graph but semantically distinct from object nodes.

### D3: Add `RelationshipWeight` to `UnifiedSearchWeights`

**Decision:** Add `RelationshipWeight float32` field to the existing `UnifiedSearchWeights` struct. When unset (zero value), default to the value of `GraphWeight` for backward compatibility.

**Approach:** In `fuseWeighted`, compute three-way normalization: `totalWeight = graphWeight + textWeight + relationshipWeight`, then normalize each. If `RelationshipWeight` is 0 (omitted by client), set it to `graphWeight` before normalizing. This preserves existing behavior where relationships used `graphWeight`.

**Alternatives considered:**

- _Normalize only graphWeight + textWeight, apply relationship weight separately:_ Would make the weight semantics inconsistent — graph and text weights would sum to 1.0, but relationship weight would be an independent multiplier. Confusing API.
- _Require all three weights explicitly:_ Breaking change for existing clients that only send `graphWeight` and `textWeight`.

**Why this approach:** Additive field with sensible default. Existing `{"graphWeight": 0.6, "textWeight": 0.4}` requests produce identical results (relationship gets 0.6, total normalizes to 0.6+0.4+0.6=1.6, graph=0.375, text=0.25, rel=0.375 — wait, this changes behavior).

**Correction:** The backward-compatible default needs more care. Today, relationships use `graphWeight` with two-way normalization. To preserve exact behavior when `relationshipWeight` is omitted, we should NOT include relationships in normalization when `relationshipWeight` is zero. Instead:

1. If `RelationshipWeight > 0`: three-way normalize all three weights.
2. If `RelationshipWeight == 0` (omitted): two-way normalize `graphWeight + textWeight` as today, apply `graphWeight` (post-normalization) to relationships.

This preserves exact backward compatibility while allowing opt-in three-way control.

### D4: Set `ivfflat.probes = 10` via `SET LOCAL` in vector search queries

**Decision:** Execute `SET LOCAL ivfflat.probes = 10` within each transaction that performs an IVFFlat vector search. `SET LOCAL` scopes the setting to the current transaction only, preventing cross-request interference.

**Approach:** Add a helper function `setIVFFlatProbes(ctx, tx, probes int)` that runs the `SET LOCAL` statement. Call it at the start of `SearchRelationships`, `VectorSearch`, `HybridSearch`, and any other repository function that queries `*_embedding` columns with `<=>` (cosine distance operator).

**Where to set it:**

- `search/repository.go`: `SearchRelationships()`, `HybridSearch()`, `VectorSearch()`
- `graph/repository.go`: `VectorSearchObjects()`, `SimilarObjects()`, and the vector component of `HybridSearch()`

**Probes value (10):** With ~100 IVFFlat lists (default for typical row counts), probes=10 scans 10% of the index. This is the standard recommendation for balancing recall vs speed. At our data scale (thousands to low tens-of-thousands of rows per project), the additional scan cost is negligible (<1ms extra) while recall improves substantially (from ~40% at probes=1 to ~85-95% at probes=10).

**Alternatives considered:**

- _Set globally via postgresql.conf:_ Affects all connections including non-search queries. Over-broad.
- _Set per-connection via connection pool hook:_ Would affect all queries on that connection, not just vector searches. Also depends on pool implementation.
- _Make probes configurable per request:_ Premature. 10 is a safe default. Can add configurability later if needed.

### D5: Query-aware expansion via similarity-scored edge sorting within existing BFS

**Decision:** Add an optional `QueryContext string` field to `ExpandParams`. When non-empty, embed it once, then at each BFS level sort the fetched edges by cosine similarity to the query vector before processing them. The BFS structure, depth limits, node limits, and edge limits remain unchanged.

**Approach:**

1. Add `QueryContext string` and `QueryVector []float32` to `ExpandParams`.
2. In `graph/service.go`'s `ExpandGraph` wrapper, if `QueryContext` is non-empty and `QueryVector` is nil, call `embeddings.EmbedQuery()` once and set `QueryVector`.
3. In `graph/repository.go`'s `ExpandGraph`, after fetching relationships for the current BFS level (line 1603), if `QueryVector` is non-nil:
   - For each relationship with a non-null `embedding`, compute cosine similarity to `QueryVector`.
   - For relationships with null embeddings, assign similarity 0.0.
   - Sort relationships by similarity descending before processing them into `result.Edges` and `neighborIDs`.
4. This means when `MaxEdges` or `MaxNodes` limits cause truncation, the most query-relevant edges survive.

**Why sort at the application layer, not in SQL:**

- The edges are already fetched in bulk per BFS level (single query per level). Sorting ~10-100 edges in Go is sub-microsecond.
- A SQL `ORDER BY embedding <=> query_vector` would require passing the vector into the query and would fight with the BFS-level batching pattern.
- The relationship embeddings are already loaded as part of the `GraphRelationship` model (the `Embedding` field exists on the entity).

**Cosine similarity computation:** Use the standard dot-product formula on unit-normalized vectors. The Vertex AI embedding model produces normalized vectors, so `similarity = dot(a, b)`. Add a small utility function `cosineSimilarity(a, b []float32) float32` in `graph/repository.go` or a shared `pkg/mathutil` package.

**Alternatives considered:**

- _Priority queue (heap) instead of sort:_ For ~10-100 edges per level, `sort.Slice` is simpler and equally fast. A heap only helps with very large edge sets, which are bounded by `MaxEdges` anyway.
- _Re-rank nodes instead of edges:_ Nodes don't have embeddings in the same way — they have `embedding_v2` but those are object embeddings, not relationship-specific. Ranking edges is more semantically meaningful for "which paths to follow."
- _Query the DB with `ORDER BY embedding <=> ?`:_ Would require restructuring the BFS query to include vector similarity ordering, complicating the direction/type/branch filtering logic. Not worth it for the typical edge count per level.

### D6: Where to put the cosine similarity utility

**Decision:** Add `cosineSimilarity` as an unexported function in `graph/repository.go` alongside `ExpandGraph`. If a second consumer emerges, promote to `pkg/mathutil`.

**Why:** Only one call site initially. Creating a new package for a 10-line function is over-engineering. The function is trivial enough that co-locating it with its consumer is clearest.

## Risks / Trade-offs

**[Embedding failure blocks all three searches] → Mitigation:** If the single `EmbedQuery()` call fails, fall back to lexical-only mode for all three goroutines (same as today's per-goroutine fallback, but applied uniformly). Log a warning. This is actually _better_ than today where one goroutine might succeed and others fail, producing inconsistent results.

**[IVFFlat probes=10 slightly increases query latency] → Mitigation:** At our data scale (<50k rows per project per table), the additional scan cost is <1ms. The recall improvement (40% → 90%+) far outweighs the cost. Monitor via existing query timing in search metadata response.

**[Three-way interleave changes result ordering for existing interleave users] → Mitigation:** This is technically a behavior change for `interleave`, `graph_first`, and `text_first` strategies. However, the current behavior _silently drops_ relationship results, which is a bug, not a feature. Users choosing these strategies likely don't expect results to be discarded. Document the fix in release notes.

**[Query-aware expansion adds embedding latency on expansion requests] → Mitigation:** Only when `queryContext` is provided (opt-in). Single embedding call (~200ms) amortized across the entire expansion traversal. For search-initiated expansion (where a query embedding already exists), pass the pre-computed vector to avoid even this cost.

**[Relationship embeddings may be null for older data] → Mitigation:** The design explicitly handles null embeddings — they get similarity score 0.0 in expansion and are excluded in vector search (existing behavior). No backfill required, though running the embedding job on older relationships would improve results.

## Migration Plan

No database migration required. All changes are code-only:

1. **Deploy normally** — no feature flags needed since all changes are backward-compatible
2. **IVFFlat probes** takes effect immediately on next search query (session-level setting)
3. **Rollback** — standard code rollback. No data changes to undo.

## Open Questions

1. **Should unified search from chat (`chat/handler.go:497`) also get the embedding deduplication?** It constructs a `UnifiedSearchRequest` and calls `searchSvc.Search()` — so yes, it gets the fix automatically. No separate work needed.
2. **Should MCP search tools (`mcp/service.go`) pass `queryContext` when expanding after search?** This would be a natural enhancement but may be out of scope for this change. Decision: include it as a task since the MCP service already has the query string available.
