## Context

**Current State:**

- Graph relationships stored in `kb.graph_relationships` with source, destination, type, and properties
- Search uses `kb.graph_objects.embedding_vec` (vector(768)) for semantic similarity
- Relationships not embedded or searchable via vector similarity
- LLM context includes only matched objects, not the relationships connecting them

**Proposal:**
Implement Cognee Phase 2 (Triplet Embedding) to embed graph relationships as natural language triplets and enable relationship-aware search.

**Constraints:**

- Must maintain backward compatibility (existing APIs, no breaking changes)
- Use existing Vertex AI embedding service (text-embedding-004, 768 dimensions)
- Leverage existing pgvector infrastructure (ivfflat indexes)
- Keep relationship creation latency acceptable (\u003c300ms p95)
- Minimize storage overhead (target \u003c5KB per relationship)

## Goals / Non-Goals

**Goals:**

- Enable semantic search across graph relationships via vector embeddings
- Generate natural language triplet text from relationships (e.g., "Elon Musk founded Tesla")
- Extend hybrid search to return both objects AND relationships ranked by relevance
- Include relationship triplets in LLM context for richer answers
- Achieve 10-20% search precision improvement (per Cognee analysis)

**Non-Goals:**

- Custom embedding models (use existing Vertex AI service)
- Complex NLP for triplet generation (use simple template: "{source} {relation} {target}")
- Relationship deduplication or merging (out of scope)
- Multi-language triplet generation (English only for Phase 2)
- Real-time re-embedding of existing relationships (batch migration acceptable)

## Decisions

### Decision 1: Triplet Text Format

**Choice:** Simple template-based generation: `"{source.name} {humanized_relation_type} {target.name}"`

**Rationale:**

- **Simplicity:** No NLP dependencies, deterministic output
- **Cognee Precedent:** Proven effective in Cognee's production system
- **Performance:** Instant generation (no external API calls for text formatting)
- **Maintainability:** Easy to debug and modify template

**Alternatives Considered:**

- **LLM-generated descriptions:** Rejected due to cost (extra API call per relationship) and latency
- **Property injection:** Rejected for Phase 2 (adds complexity, diminishing returns)

**Implementation:**

```go
// Example: WORKS_FOR → "works for"
func humanizeRelationType(relType string) string {
    return strings.ToLower(strings.ReplaceAll(relType, "_", " "))
}

// Example: "Elon Musk founded Tesla"
func generateTripletText(source, target *GraphObject, relType string) string {
    return fmt.Sprintf("%s %s %s",
        source.Properties["name"],
        humanizeRelationType(relType),
        target.Properties["name"])
}
```

### Decision 2: Embedding Generation Timing

**Choice:** Synchronous during relationship creation (within same transaction)

**Rationale:**

- **Data Consistency:** Embedding always exists with relationship (no orphaned records)
- **Simplicity:** Single transaction, no job queue complexity
- **Acceptable Latency:** Vertex AI embedding ~100ms p50, \u003c200ms p95
- **Error Handling:** Failed embedding = failed relationship creation (clear contract)

**Alternatives Considered:**

- **Async job queue:** Rejected for Phase 2 (adds complexity, eventual consistency issues)
- **Batch embedding:** Rejected (leaves window where relationships lack embeddings)

**Trade-off:**

- Adds ~100-200ms to relationship creation latency
- Mitigation: This is acceptable for graph mutation operations (not on hot path)

### Decision 3: Null Embedding Handling

**Choice:** Allow `embedding IS NULL` initially, backfill via migration, make required for NEW relationships

**Rationale:**

- **Graceful Migration:** Existing relationships won't break
- **Forward Compatibility:** All new relationships must have embeddings
- **Search Resilience:** Search queries filter `WHERE embedding IS NOT NULL`

**Implementation:**

```sql
-- Migration adds column as nullable
ALTER TABLE kb.graph_relationships ADD COLUMN embedding vector(768);

-- Application enforces non-null for new relationships
-- Backfill script updates existing relationships
```

### Decision 4: Search Result Merging Strategy

**Choice:** Query nodes and edges separately, merge via Reciprocal Rank Fusion (RRF)

**Rationale:**

- **Proven Algorithm:** RRF used successfully in existing hybrid search (FTS + vector)
- **Fair Ranking:** Balances node vs edge relevance without bias
- **Performance:** Two parallel queries faster than complex JOIN
- **Flexibility:** Easy to adjust weights if needed

**Alternatives Considered:**

- **Single unified query:** Rejected (complex SQL, harder to optimize)
- **Weighted merging:** Deferred to Phase 3 (RRF baseline first)

**Implementation:**

```go
// Parallel queries
nodeResults := searchNodes(ctx, queryVector, limit)
edgeResults := searchEdges(ctx, queryVector, limit)

// Merge with RRF (k=60, same as existing hybrid search)
merged := reciprocalRankFusion(nodeResults, edgeResults, k=60)
return merged[:limit]
```

### Decision 5: Index Strategy

**Choice:** Single ivfflat index on `embedding` column with `lists=100`

**Rationale:**

- **Consistency:** Matches `graph_objects.embedding_vec` index configuration
- **Proven Performance:** ivfflat handles 768-dimensional vectors efficiently
- **Build Time:** ~10 minutes for 1M relationships (acceptable one-time cost)

**Alternatives Considered:**

- **HNSW index:** Rejected (not yet stable in pgvector, migration risk)
- **Multiple indexes:** Rejected (unnecessary for Phase 2 scale)

**Configuration:**

```sql
CREATE INDEX idx_graph_relationships_embedding
ON kb.graph_relationships
USING ivfflat (embedding vector_cosine_ops)
WITH (lists = 100);
```

### Decision 6: Backfill Strategy

**Choice:** Optional backfill script (run post-deployment, low priority)

**Rationale:**

- **Non-Blocking:** New relationships get embeddings automatically
- **Gradual Improvement:** Search improves as more relationships embedded
- **Resource Control:** Backfill during off-peak hours

**Implementation:**

```bash
# Run backfill (processes relationships without embeddings)
nx run server:backfill-relationship-embeddings --batch-size=100 --delay=100ms
```

## Risks / Trade-offs

### Risk 1: Embedding Service Quota Limits

**Risk:** Vertex AI rate limits could throttle relationship creation at scale  
**Mitigation:**

- Monitor quota usage via Cloud Monitoring
- Implement exponential backoff for 429 errors
- Document quota requirements in deployment guide
- For backfill: use small batches (100) with delays (100ms)

### Risk 2: Index Build Time on Large Datasets

**Risk:** Initial index creation blocks writes (exclusive lock)  
**Mitigation:**

- Use `CREATE INDEX CONCURRENTLY` to avoid blocking (Postgres 11+)
- Schedule index build during maintenance window
- Document expected build time (10 min per 1M relationships)

### Risk 3: Search Latency Increase

**Risk:** Hybrid search (nodes + edges) adds 50-100ms latency  
**Mitigation:**

- Acceptable for improved precision (10-20% gain)
- Monitor p95/p99 latencies post-deployment
- Consider edge result limit tuning if needed (current: 50)

### Risk 4: Name Property Assumptions

**Risk:** Triplet generation assumes `source.properties["name"]` and `target.properties["name"]` exist  
**Mitigation:**

- Fallback to object `key` if `name` property missing
- Log warning when name missing (for data quality monitoring)
- Validation in tests (ensure name or key present)

**Code:**

```go
func getDisplayName(obj *GraphObject) string {
    if name, ok := obj.Properties["name"].(string); ok && name != "" {
        return name
    }
    return obj.Key // Fallback to key
}
```

### Risk 5: Storage Growth

**Risk:** +3KB per relationship could impact large graphs  
**Trade-off:**

- 1M relationships = +3GB storage (acceptable)
- Storage cheaper than compute/API calls
- pgvector compression applies automatically

## Migration Plan

**Phase 1: Database Schema (Week 1, Day 1)**

1. Create migration: `20260212_add_relationship_embeddings.sql`
2. Add nullable `embedding vector(768)` column
3. Apply to dev/staging environments
4. Monitor for issues (query plan changes, lock contention)

**Phase 2: Application Deployment (Week 1, Day 2-3)**

1. Deploy updated `graph.Service.CreateRelationship()` with embedding generation
2. Deploy updated `search.Repository.Search()` with edge queries
3. Monitor relationship creation latency (target: p95 \u003c 300ms)
4. Monitor search latency (target: p95 increase \u003c 100ms)

**Phase 3: Index Creation (Week 1, Day 4)**

1. Run index creation during off-peak hours:
   ```sql
   CREATE INDEX CONCURRENTLY idx_graph_relationships_embedding
   ON kb.graph_relationships
   USING ivfflat (embedding vector_cosine_ops)
   WITH (lists = 100);
   ```
2. Monitor index build progress (`pg_stat_progress_create_index`)
3. Verify query plans use index (`EXPLAIN ANALYZE`)

**Phase 4: Backfill (Week 2, Low Priority)**

1. Create backfill script: `apps/server-go/cmd/backfill-embeddings/main.go`
2. Run in batches (100 relationships per batch, 100ms delay)
3. Monitor Vertex AI quota consumption
4. Track progress (relationships processed / total)

**Rollback Strategy:**

- Embedding generation failure → relationship creation fails (clean rollback via transaction)
- Search errors → Query falls back to object-only search (graceful degradation)
- Index issues → Drop index, search bypasses edge queries (feature disable)

## Open Questions

**Q1:** Should we re-embed relationships when source/target names change?  
**Current Decision:** No for Phase 2 (adds complexity). Consider for Phase 3 if needed.

**Q2:** What's the minimum similarity threshold for relationship results?  
**Current Decision:** Use same threshold as objects (0.7). Tune based on production metrics.

**Q3:** Should we expose relationship embeddings via API?  
**Current Decision:** No for Phase 2 (internal implementation detail). Can expose later if needed.

**Q4:** How to handle relationships between objects without names?  
**Current Decision:** Fallback to object `key` field. Log warnings for monitoring.
