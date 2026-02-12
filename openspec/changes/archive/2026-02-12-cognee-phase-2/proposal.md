## Why

Current search only finds entities (graph objects) via vector similarity, missing relationships between entities. Users cannot answer questions like "Who founded Tesla?" or "What companies are in San Francisco?" because relationship semantics are not embedded or searchable. Phase 2 (Triplet Embedding) from the Cognee analysis addresses this by embedding graph edges alongside nodes, enabling semantic search on relationships.

## What Changes

- Add `embedding` column (vector(768)) to `kb.graph_relationships` table
- Generate natural language triplet text for each relationship (e.g., "Elon Musk founded Tesla")
- Embed triplet text during relationship creation using existing Vertex AI embedding service
- Extend search to query both graph objects AND relationships via vector similarity
- Combine and rank node + edge results in hybrid search responses
- Include relevant relationships in LLM context for better answers

## Capabilities

### New Capabilities

- `relationship-embedding`: Automatic embedding generation for graph relationships using triplet text format (source + relation + target)
- `relationship-search`: Vector similarity search across graph edges with combined node/edge ranking

### Modified Capabilities

- `graph-search`: Extends existing hybrid search to include relationship embeddings alongside object embeddings

## Impact

**Affected Code:**

- `apps/server-go/domain/graph/entity.go` - Add `Embedding` field to `GraphRelationship` struct
- `apps/server-go/domain/graph/service.go` - Generate triplet text and call embedding service during relationship creation
- `apps/server-go/domain/search/repository.go` - Add relationship vector search query, merge with node results
- Database migration to add embedding column + ivfflat index

**APIs:**

- Existing relationship creation endpoint behavior unchanged (backward compatible)
- Existing search endpoint returns enhanced results (relationships in context) without breaking changes

**Dependencies:**

- Leverages existing Vertex AI embedding service (text-embedding-004, 768 dimensions)
- Uses existing pgvector infrastructure (ivfflat indexes, cosine similarity)

**Performance:**

- Embedding generation adds ~100-200ms per relationship creation (async, non-blocking)
- Hybrid search latency increases ~50-100ms (acceptable for better precision)
- Storage: +3KB per relationship (768 floats × 4 bytes)
- Initial index build: ~10 minutes for 1M relationships (one-time cost)

**Benefits:**

- 10-20% search precision improvement (per Cognee analysis)
- Richer LLM context with relationship triplets
- Support for relationship-centric queries ("founded by", "located in", "works for")
