## Why

Vector similarity search appends a secondary `id ASC` sort key after the cosine-distance
sort to break exact distance ties deterministically. PostgreSQL refuses to use the pgvector
HNSW index when a non-distance secondary sort key is present, so those queries fall back to
a parallel sequential scan + sort. On a ~101k-object project this makes hybrid search take
~130s instead of ~4ms (verified via EXPLAIN: `ORDER BY distance ASC, id ASC` → Seq Scan;
`ORDER BY distance ASC` → HNSW index scan).

## What Changes

Drop the `, id ASC` secondary sort key from every vector/hybrid `ORDER BY` so the HNSW
index is selected. The determinism guarantee is preserved where it is already handled
outside SQL (fused-search paths re-sort in Go), and for the two `LIMIT 1` merge-dedup
queries by wrapping the ANN query in an overfetch subquery whose outer query re-applies a
deterministic `distance ASC, id ASC` tiebreak.

- **Graph objects** `VectorSearch` / `FindSimilarObjects`: `ORDER BY distance ASC` (caller
  overfetches and re-sorts by fused score then id).
- **Graph objects** `FindSimilarObjectInBranch` (`embedding_v2`): overfetch `LIMIT 64`
  inner ANN (ordered by distance only) → outer `ORDER BY _dist ASC, id ASC LIMIT 1`.
- **Graph relationships** `FindSimilarRelationshipInBranch` (`embedding`): same overfetch +
  outer-sort shape.
- **Chunk** `VectorSearch` / hybrid vector search (`kb.chunks.embedding`): `ORDER BY
  c.embedding <=> ?::vector` (hybrid fusion already tiebreaks by id in Go).
- **Relationship** `buildRelationshipSearchQuery` (`kb.graph_relationships.embedding`):
  `ORDER BY r.embedding <=> ?::vector` (RRF/weighted fusion re-ranks deterministically).

## Capabilities

### Modified Capabilities

- `retrieval-performance-config`: add the ORDER-BY constraint — vector/hybrid search
  `ORDER BY` MUST NOT append a non-distance secondary sort key, so the HNSW index is
  selected (optionally via an overfetch subquery whose outer query re-applies the
  deterministic distance-then-id tiebreak).

## Impact

- **Server** `apps/server/domain/graph/repository.go`: drop the `id ASC` tiebreak from
  `VectorSearch`/`FindSimilarObjects`; rewrite `FindSimilarObjectInBranch` and
  `FindSimilarRelationshipInBranch` as overfetch + outer-sort.
- **Server** `apps/server/domain/search/repository.go`: drop the `id ASC` tiebreak from
  chunk vector/hybrid search and relationship ANN search.
- **Behaviour**: unchanged — deterministic id tiebreaking is preserved either in Go fusion
  re-sorts or in the outer sort of the overfetch wrapper.
