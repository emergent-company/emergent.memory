## Purpose

Extends `retrieval-performance-config` with the query-shape constraint that keeps the
pgvector HNSW indexes selected: vector and hybrid search `ORDER BY` clauses MUST order by
the cosine-distance expression only, never appending a non-distance secondary sort key.

## ADDED Requirements

### Requirement: Vector-search ORDER BY must not append a non-distance secondary sort key

Graph-object (`kb.graph_objects.embedding_v2`), relationship
(`kb.graph_relationships.embedding`), and chunk (`kb.chunks.embedding`) vector/hybrid
search queries MUST order by the cosine-distance expression only, so the HNSW index is
selected. Deterministic id tiebreaking is preserved either by re-sorting in Go (fused
paths) or, for a single-result (`LIMIT 1`) ANN query, by wrapping it in an overfetch
subquery ordered by distance only and re-applying the deterministic `distance ASC, id ASC`
tiebreak in the outer query.

#### Scenario: Graph-object vector search uses the HNSW index

- **WHEN** a graph-object vector or hybrid search runs
- **THEN** its `ORDER BY` MUST be the cosine-distance expression only, with no `id` sort
  key, so the HNSW index on `kb.graph_objects.embedding_v2` is used

#### Scenario: Relationship vector search uses the HNSW index

- **WHEN** a relationship vector search runs
- **THEN** its `ORDER BY` MUST be the `r.embedding <=> ?::vector` expression only, with no
  `r.id` sort key, so the HNSW index on `kb.graph_relationships.embedding` is used

#### Scenario: Chunk vector and hybrid search use the HNSW index

- **WHEN** a chunk vector or hybrid search runs
- **THEN** its `ORDER BY` MUST be the `c.embedding <=> ?::vector` expression only, with no
  `c.id` sort key, so the HNSW index on `kb.chunks.embedding` is used

#### Scenario: Deterministic tiebreak preserved for single-result ANN queries

- **WHEN** a `LIMIT 1` nearest-match ANN query runs (e.g. similarity merge)
- **THEN** it MUST use an inner overfetch subquery ordered by distance only (so the HNSW
  index is selected) and an outer query that re-applies the deterministic
  `distance ASC, id ASC` tiebreak before taking the single row
