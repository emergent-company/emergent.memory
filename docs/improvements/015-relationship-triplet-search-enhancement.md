# Improvement Suggestion: Enhance Relationship Triplet Embedding Usage in Search & RAG

**Status:** Proposed
**Priority:** Medium
**Category:** Performance / Architecture
**Proposed:** 2026-02-14
**Proposed by:** AI Agent (comparative analysis of FalkorDB GraphRAG patterns)
**Assigned to:** Unassigned

---

## Summary

Relationship triplet embeddings ("Elon Musk founded Tesla") are generated and indexed but underutilized. This proposal addresses five concrete gaps: redundant embedding calls, low IVFFlat recall, dropped relationship results in fusion strategies, missing edge-aware graph expansion, and a missing independent weight for relationship results.

---

## Current State

### What Works Well

Emergent already has a solid relationship triplet embedding system:

- **Generation**: `graph/service.go` generates triplet text via `generateTripletText()` during `CreateRelationship`. Text is humanized (`WORKS_FOR` -> `"works for"`) and structured as `"Subject verb Object"`.
- **Storage**: `kb.graph_relationships.embedding` column (vector(768)) with IVFFlat index (100 lists, cosine ops).
- **Search**: `search/repository.go:SearchRelationships` performs vector similarity on the relationship embedding column, joining with `graph_objects` to reconstruct the source/target context.
- **Unified Search**: `search/service.go` orchestrates 3 parallel searches (graph objects, text chunks, relationships) and fuses results.
- **RAG Integration**: MCP tools call unified search, so relationship results do reach the LLM in chat.

### Gaps Identified

| #   | Gap                                                                                                                                        | Impact                                                                                                       |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| 1   | **3 redundant `EmbedQuery()` calls** per unified search — each of the 3 parallel goroutines independently embeds the same query string     | ~2 extra embedding API round-trips per search (~200-400ms wasted latency)                                    |
| 2   | **`ivfflat.probes = 1` (default)** on all 3 vector indexes                                                                                 | Low recall — the index scans only 1 of 100 lists. Relevant results are missed silently.                      |
| 3   | **Relationship results dropped by 3 of 5 fusion strategies** — `interleave`, `graph_first`, and `text_first` only use graph + text results | Relationship search runs (costing an embedding + DB query) but results are thrown away for most fusion modes |
| 4   | **Graph expansion is edge-blind** — `ExpandGraph` BFS follows all edges equally via `src_id`/`dst_id`                                      | No query-aware ranking. A search for "funding relationships" traverses employment edges with equal priority. |
| 5   | **No independent weight for relationship results** in `weighted` fusion — they share `graphWeight`                                         | Cannot independently tune how much relationship triplet matches influence final ranking                      |

---

## Proposed Improvement

### Fix 1: Deduplicate Query Embedding (Low effort)

Extract the `EmbedQuery()` call to run once before launching the 3 parallel goroutines. Pass the pre-computed embedding vector to each search function.

```
Before:  goroutine1{embed+searchGraphs} || goroutine2{embed+searchText} || goroutine3{embed+searchRels}
After:   embed -> goroutine1{searchGraphs(vec)} || goroutine2{searchText(vec)} || goroutine3{searchRels(vec)}
```

**Expected impact:** ~200-400ms latency reduction per unified search. Reduces Vertex AI embedding API costs by ~66%.

### Fix 2: Increase IVFFlat Probes (Trivial effort)

Set `ivfflat.probes` to 10-20 at session level or before vector queries. With 100 lists, probes=10 scans 10% of the index for significantly better recall with modest latency increase.

```sql
SET ivfflat.probes = 10;
```

**Expected impact:** Materially better recall on all vector searches (objects, chunks, relationships). Latency increase of ~5-15ms per query (scanning 10 lists instead of 1).

**Alternative:** Migrate from IVFFlat to HNSW indexes. HNSW has better recall characteristics without needing to tune probes, at the cost of higher memory usage and slower index builds. Worth evaluating once dataset sizes justify it.

### Fix 3: Include Relationships in All Fusion Strategies (Low effort)

Update `interleave`, `graph_first`, and `text_first` fusion strategies to include relationship results. For `interleave`, add them to the round-robin. For `graph_first`/`text_first`, append them after the primary source.

Alternatively, skip the relationship search goroutine entirely when the fusion strategy won't use the results (saves compute).

**Expected impact:** Either better search results (if included) or lower latency (if skipped).

### Fix 4: Query-Aware Edge Ranking in Graph Expansion (Medium effort)

When `ExpandGraph` is called with a search query context, use relationship embeddings to score edges and prioritize traversal. Instead of pure BFS where all edges are equal, use a priority queue weighted by cosine similarity between the query embedding and each edge's triplet embedding.

```
Current:  BFS queue: [all edges from node, unranked]
Proposed: Priority queue: [edges sorted by similarity(query_embedding, edge.embedding)]
```

This makes expansion query-aware: searching for "acquisition history" would preferentially traverse `acquired`, `merged_with` edges over `employs`, `located_in` edges.

**Expected impact:** More relevant subgraphs returned for query-driven expansion. Particularly valuable for RAG where the expanded context is fed to the LLM. May increase latency slightly per expansion (extra vector comparison per edge), but reduces noise in results.

### Fix 5: Independent Relationship Weight in Fusion (Trivial effort)

Add a `relationshipWeight` parameter to the weighted fusion strategy, defaulting to the same value as `graphWeight` for backward compatibility.

**Expected impact:** Fine-grained control over triplet match influence in search ranking.

---

## Performance Analysis

### Current Search Architecture Performance

The unified search runs 3 parallel goroutines, each doing:

1. Embed query via Vertex AI API (~100-200ms)
2. Run PostgreSQL vector query with IVFFlat index (~5-20ms at probes=1)
3. Return scored results

Total wall-clock for unified search: ~200-400ms (dominated by embedding latency).

### Performance Impact of Proposed Changes

| Change                     | Latency Impact             | Recall Impact                 | API Cost Impact                 |
| -------------------------- | -------------------------- | ----------------------------- | ------------------------------- |
| Deduplicate embedding      | **-200-400ms**             | None                          | **-66% embedding calls**        |
| Increase probes to 10      | +5-15ms per vector query   | **Significant improvement**   | None                            |
| Include rels in all fusion | None (already computed)    | **Better for 3 fusion modes** | None                            |
| Query-aware expansion      | +2-5ms per expansion level | **More relevant subgraphs**   | +1 embedding call if not cached |
| Independent rel weight     | None                       | Tunable                       | None                            |

**Net effect:** Faster (by ~200ms+), better recall, lower cost, more relevant results.

### Comparison to FalkorDB's Approach

FalkorDB uses sparse matrix linear algebra (GraphBLAS) for graph traversal. Their advantage is primarily in **pure traversal speed** — sub-millisecond for multi-hop path queries on large graphs. However:

- **Emergent's bottleneck is not traversal** — it's the embedding API call (~200ms). The actual PostgreSQL graph queries run in 5-20ms. FalkorDB's sub-millisecond traversal would save 5-19ms — negligible compared to the embedding latency.
- **FalkorDB has no built-in vector search** — it relies on RediSearch's VectorSimilarity, which is roughly comparable to pgvector with IVFFlat/HNSW.
- **FalkorDB has no built-in embedding generation** — the GraphRAG-SDK calls external LLM APIs, same latency as Emergent.
- **Where FalkorDB would win:** Multi-hop traversals across very large graphs (millions of nodes/edges). Cypher's `MATCH (a)-[:KNOWS*3..5]->(b)` is inherently faster with sparse matrix multiplication than recursive SQL CTEs. At Emergent's current scale (project-scoped graphs, typically thousands to tens of thousands of nodes), PostgreSQL's BFS with `FOR`/`JOIN` is adequate.
- **Where PostgreSQL wins:** Transactional consistency, RLS, pgvector integration, no additional infrastructure. FalkorDB would add Redis as a dependency and require data synchronization between PostgreSQL (source of truth) and Redis (graph engine).

**Bottom line:** Adopting FalkorDB's graph engine is not justified at current scale. The proposed improvements to triplet search would deliver more practical value than a database migration.

---

## Schema Comparison: FalkorDB Ontology vs Emergent Template Packs

### Structural Mapping

| Concept                | FalkorDB Ontology                                                         | Emergent Template Pack                                                                                              | Difference                                                                                 |
| ---------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| **Container**          | `Ontology(entities, relations)`                                           | `GraphTemplatePack(object_type_schemas, relationship_type_schemas)`                                                 | Equivalent                                                                                 |
| **Entity type**        | `Entity(label, attributes, description)`                                  | `ObjectSchema(name, properties, required, description, extraction_guidelines)`                                      | Emergent adds `extraction_guidelines`                                                      |
| **Relationship type**  | `Relation(label, source, target, attributes)`                             | `RelationshipSchema(name, source_types[], target_types[], description, extraction_guidelines)`                      | Emergent supports **multiple** source/target types per relationship; FalkorDB is 1:1       |
| **Property/Attribute** | `Attribute(name, type, unique, required)`                                 | `PropertyDef(type, description)` + separate `required[]` array                                                      | FalkorDB has `unique` constraint; Emergent has per-property `description`                  |
| **Type system**        | `string, number, boolean` (LLM), + `list, point, map, vectorf32` (import) | `string, number, boolean, date, array, object`                                                                      | Emergent has `date`, `array`, `object`; FalkorDB has `point`, `vector`                     |
| **Naming conventions** | Labels: TitleCase, Relations: UPPERCASE, Attributes: snake_case           | Type names: free-form (typically UPPERCASE), Properties: free-form                                                  | FalkorDB enforces conventions at SDK level                                                 |
| **Versioning**         | None — ontology is mutable                                                | `version`, `parent_version_id`, `superseded_by` — full version chain                                                | Emergent has proper versioning                                                             |
| **Installation**       | Implicit (per KnowledgeGraph)                                             | Explicit install per project with `enabledTypes`, `disabledTypes`, `schemaOverrides`                                | Emergent has multi-tenancy and customization                                               |
| **Schema evolution**   | `merge_with()` — additive only (adds missing attributes, never removes)   | `SchemaMigrator` — risk assessment, field archival, rollback                                                        | Emergent has proper migration                                                              |
| **Discovery**          | `Ontology.from_sources()` — LLM auto-detect                               | `source: 'discovered'` field exists, `discovery_job_id` column exists, but **no implementation found in Go server** | FalkorDB has working auto-detect; Emergent has the schema for it but no implementation yet |

### Key Takeaway

Emergent's template pack system is **more mature** than FalkorDB's ontology in every dimension except one: **automated discovery from documents**. Emergent has versioning, migration, per-project customization, validation, and extraction guidelines — features FalkorDB's ontology completely lacks. The one gap is the auto-detection step, which you mentioned wanting to build as semi-automatic schema evolution (user confirms changes).

---

## Benefits

- **User Benefits:** Better search results through improved recall and relationship-aware retrieval. More relevant graph expansion context for RAG answers.
- **Developer Benefits:** Cleaner search architecture (no redundant API calls). Independent tuning knobs for relationship vs object search weighting.
- **System Benefits:** ~66% reduction in embedding API calls during search. Better utilization of existing infrastructure (relationship embeddings are already computed and indexed).
- **Business Benefits:** Lower Vertex AI costs. Better RAG quality directly improves the core product value proposition.

---

## Implementation Approach

### Phase 1: Quick Wins (Small effort, 1-2 days)

1. Deduplicate `EmbedQuery()` call in unified search — embed once, pass vector to all 3 goroutines
2. Set `ivfflat.probes = 10` for vector search sessions
3. Add `relationshipWeight` parameter to weighted fusion (default = `graphWeight`)
4. Either include relationship results in all fusion strategies or skip the goroutine when unused

### Phase 2: Query-Aware Expansion (Medium effort, 3-5 days)

5. Modify `ExpandGraph` to accept an optional query embedding parameter
6. When provided, fetch relationship embeddings during expansion and use cosine similarity to score edges
7. Use a priority queue instead of FIFO for BFS traversal
8. Expose via API as an optional `queryContext` parameter on expansion endpoints

**Affected Components:**

- `apps/server-go/domain/search/service.go` — unified search orchestrator
- `apps/server-go/domain/search/repository.go` — relationship search queries
- `apps/server-go/domain/graph/repository.go` — ExpandGraph, vector search functions
- `apps/server-go/domain/graph/service.go` — hybrid search fusion
- `apps/server-go/domain/graph/dto.go` — new parameters
- `apps/server-go/domain/mcp/service.go` — MCP tool updates

**Estimated Effort:** Small (Phase 1) + Medium (Phase 2)

---

## Risks & Considerations

- **Breaking Changes:** No — all changes are additive or behavioral improvements with backward-compatible defaults
- **Performance Impact:** Positive — faster search, better recall, lower API costs
- **Security Impact:** Neutral — no auth or access control changes
- **Dependencies:** None — uses existing pgvector, existing embedding infrastructure
- **Migration Required:** No — no schema changes needed. IVFFlat probes is a runtime setting.

---

## Success Metrics

- Embedding API calls per unified search reduced from 3 to 1
- Search recall improvement measurable via relevance benchmarks (before/after probes change)
- Relationship results appearing in search output for fusion strategies that currently drop them
- RAG answer quality for relationship-oriented questions (e.g., "who founded X?", "what acquired Y?")

---

## Testing Strategy

- [x] Existing E2E tests cover search and graph expansion (609 tests)
- [ ] Add benchmark comparing search recall at probes=1 vs probes=10
- [ ] Add test verifying relationship results appear in all fusion strategies
- [ ] Add test for query-aware expansion edge ranking
- [ ] Performance regression test for unified search latency

---

## Related Items

- Related to schema evolution / ontology discovery (template pack `source: 'discovered'` infrastructure)
- Relationship triplet embedding generation (`graph/service.go:577`)
- Migrations `00011`, `00012`, `00013` (relationship embedding infrastructure)
- Backfill tool: `apps/server-go/cmd/backfill-embeddings/main.go`

---

## References

- FalkorDB GraphRAG-SDK: https://github.com/FalkorDB/GraphRAG-SDK
- FalkorDB text-to-cypher (self-healing query pattern): https://github.com/FalkorDB/text-to-cypher
- pgvector IVFFlat tuning: https://github.com/pgvector/pgvector#ivfflat
- GraphBLAS sparse matrix algebra: https://graphblas.org/

---

## Notes

### On Semi-Automatic Schema Evolution

The `kb.graph_template_packs` table already has `source` (enum: manual/discovered/imported/system), `discovery_job_id`, and `pending_review` columns — the infrastructure for discovered schemas exists at the DB level. The missing piece is the Go implementation:

1. A discovery job that samples document content and proposes new/modified object/relationship types
2. A review UI where users confirm, modify, or reject proposed schema changes
3. A merge step that applies confirmed changes as a new template pack version

FalkorDB's `Ontology.from_sources()` uses a simple approach: LLM reads documents, outputs JSON schema, results are merged additively. Emergent should do better:

- **Incremental discovery**: Run on new documents only, propose delta changes to existing schema
- **Confidence scoring**: Flag low-confidence type suggestions differently from high-confidence ones
- **User confirmation always required**: As you specified — the system proposes, the user decides
- **Version chain**: Each confirmed evolution creates a new template pack version with `parent_version_id` linking to the previous

This is a separate improvement from the triplet search enhancements and should be tracked independently.

---

**Last Updated:** 2026-02-14 by AI Agent
