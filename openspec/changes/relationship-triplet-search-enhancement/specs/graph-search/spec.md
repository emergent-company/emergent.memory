## MODIFIED Requirements

### Requirement: Hybrid search includes relationship embeddings alongside object embeddings

The system SHALL extend existing hybrid search to query both graph object embeddings and graph relationship embeddings, merging results via Reciprocal Rank Fusion, with improved recall via increased IVFFlat probes.

**Modified from:** IVFFlat probes at default (1), no query-aware expansion
**Modified to:** IVFFlat probes set to 10 for all vector queries, optional query-aware expansion mode

#### Scenario: Vector search sets IVFFlat probes for improved recall

- **WHEN** executing any vector similarity query against `graph_objects.embedding_v2`
- **THEN** the system SHALL set `SET LOCAL ivfflat.probes = 10` within the transaction before executing the query
- **AND** this SHALL apply to FTS+vector hybrid search, vector-only search, and similar-objects search

#### Scenario: Search queries both objects and relationships

- **WHEN** user executes graph search query
- **THEN** system runs parallel queries against both `graph_objects` (FTS + vector) and `graph_relationships` (vector only)

#### Scenario: RRF merges object and relationship results

- **WHEN** both object and relationship queries return results
- **THEN** system merges using RRF with k=60 (same algorithm as existing FTS+vector merge)

#### Scenario: Search degrades gracefully when relationship embeddings unavailable

- **WHEN** relationship search fails or returns empty (e.g., no embeddings exist yet)
- **THEN** system returns object-only search results without errors

### Requirement: Graph expansion supports query-aware edge prioritization

The system SHALL support an optional query-aware mode on graph expansion endpoints where edges are prioritized by semantic similarity to a query string.

**Modified from:** ExpandGraph uses unranked BFS traversal
**Modified to:** ExpandGraph optionally accepts queryContext for similarity-ranked edge traversal

#### Scenario: Expansion endpoint accepts queryContext

- **WHEN** a client calls the graph expansion API with `queryContext: "funding rounds"`
- **THEN** the system SHALL pass the query context to the ExpandGraph function
- **AND** edge traversal SHALL be prioritized by relationship embedding similarity to the query

#### Scenario: Expansion without queryContext unchanged

- **WHEN** a client calls graph expansion without queryContext
- **THEN** behavior SHALL be identical to current unranked BFS implementation
