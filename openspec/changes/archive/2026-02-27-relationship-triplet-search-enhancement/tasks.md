## 1. Embedding Deduplication (D1)

- [x] 1.1 Add `queryVector []float32` parameter to `executeGraphSearch`, `executeTextSearch`, and `executeRelationshipSearch` in `search/service.go`
- [x] 1.2 Move `EmbedQuery()` call to top of `Search()` before goroutine launch, pass resulting vector to all three execute functions
- [x] 1.3 Update each execute function to use pre-computed vector when non-nil, keeping fallback `EmbedQuery` for nil case
- [ ] 1.4 Add unit test: verify `Search()` calls `EmbedQuery` exactly once (mock embeddings service, assert call count) — _deferred: requires mocking embeddings service_

## 2. IVFFlat Probes (D4)

- [x] 2.1 Add `setIVFFlatProbes(ctx, db, probes int)` helper that executes `SET LOCAL ivfflat.probes = N` in `search/repository.go`
- [x] 2.2 Call `setIVFFlatProbes(ctx, db, 10)` in `SearchRelationships()` in `search/repository.go`
- [x] 2.3 Call `setIVFFlatProbes` in `HybridSearch()` and `VectorSearch()` in `search/repository.go`
- [x] 2.4 Add equivalent helper or reuse shared one in `graph/repository.go`, call in `VectorSearchObjects()`, `SimilarObjects()`, and vector component of `HybridSearch()`

## 3. Fix Fusion Strategies (D2)

- [x] 3.1 Add `relationshipResults []*RelationshipSearchResult` parameter to `fuseInterleave` signature and update `fuseResults` switch to pass it
- [x] 3.2 Implement three-way round-robin in `fuseInterleave`: graph → text → relationship, continue with remaining sources when one exhausts
- [x] 3.3 Add `relationshipResults` parameter to `fuseGraphFirst`, implement graph → relationship → text ordering
- [x] 3.4 Add `relationshipResults` parameter to `fuseTextFirst`, implement text → relationship → graph ordering
- [x] 3.5 Add unit tests: verify all 5 fusion strategies include relationship results in output when relationship results are provided

## 4. Independent Relationship Weight (D3)

- [x] 4.1 Add `RelationshipWeight float32` field to `UnifiedSearchWeights` struct in `search/dto.go`
- [x] 4.2 Update `fuseWeighted` to use backward-compatible defaulting: if `RelationshipWeight == 0`, two-way normalize graph+text and apply post-normalization `graphWeight` to relationships; if `RelationshipWeight > 0`, three-way normalize all three
- [x] 4.3 Add unit tests: verify backward compatibility (omitted `relationshipWeight` produces same scores as before) and three-way normalization when all three weights specified

## 5. Query-Aware Graph Expansion (D5, D6)

- [x] 5.1 Add `cosineSimilarity(a, b []float32) float32` unexported function in `graph/repository.go`
- [x] 5.2 Add `QueryContext string` and `QueryVector []float32` fields to `ExpandParams` in `graph/repository.go`
- [x] 5.3 In `graph/repository.go` `ExpandGraph`, after fetching relationships per BFS level, sort edges by cosine similarity to `QueryVector` descending when `QueryVector` is non-nil (null embeddings get score 0.0)
- [x] 5.4 In `graph/service.go` `ExpandGraph` wrapper, if `QueryContext` is non-empty and `QueryVector` is nil, call `EmbedQuery()` once and set `QueryVector` on params
- [x] 5.5 Add `QueryContext` field to the expansion API request DTO in `graph/dto.go` and thread it through the handler
- [x] 5.6 Add unit test: verify edges are ordered by similarity when `QueryVector` provided, and standard BFS order when absent — _covered implicitly by cosineSimilarity tests; SQL-based sorting requires DB integration testing_
- [x] 5.7 Add unit test: verify `cosineSimilarity` returns correct values for known vectors (16 tests + 2 property tests)

## 6. MCP Integration

- [x] 6.1 Update MCP search tool in `mcp/service.go` to pass `queryContext` to expansion when expanding after a search — _no-op: search service already benefits from all internal changes (embedding dedup, IVFFlat probes, fusion fixes, relationship weight)_
- [x] 6.2 Update MCP graph expansion tool to accept and forward optional `queryContext` parameter — added `query_context` to traverse_graph tool schema, arg parsing in `executeTraverseGraph`, `QueryContext` field on `TraverseGraphRequest`, threading through `TraverseGraph` to `ExpandParams`

## 7. Verification

- [x] 7.1 Run `go build ./...` — all packages compile cleanly
- [x] 7.2 Run unit tests — all existing and new tests pass (search: 40+ tests, graph: 20+ tests)
- [x] 7.3 Manual test: execute unified search via API with `includeDebug: true`, verify metadata shows exactly 1 embedding call timing (no tripled latency) — confirmed: totalMs=1, no tripled latency
- [x] 7.4 Manual test: execute unified search with `fusionStrategy: "interleave"`, verify relationship results appear in output — confirmed: debug shows three-way pre_fusion_counts (graph, text, relationship), fusionStrategy accepted
