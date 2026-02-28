# query-aware-expansion Specification

## Purpose
TBD - created by archiving change relationship-triplet-search-enhancement. Update Purpose after archive.
## Requirements
### Requirement: Query-aware edge prioritization during graph expansion

The system SHALL support an optional query-aware mode for graph expansion where relationship embeddings are used to rank and prioritize which edges to traverse, instead of treating all edges equally in BFS order.

#### Scenario: Expansion with query context prioritizes relevant edges

- **WHEN** a user calls graph expansion with `queryContext: "funding and acquisitions"` on a node connected by edges of types ACQUIRED, EMPLOYS, LOCATED_IN, and FUNDED_BY
- **THEN** the system SHALL embed the query context string
- **AND** the system SHALL compute cosine similarity between the query embedding and each outgoing edge's triplet embedding
- **AND** edges with higher similarity (ACQUIRED, FUNDED_BY) SHALL be traversed before lower-similarity edges (EMPLOYS, LOCATED_IN)

#### Scenario: Expansion without query context uses standard BFS

- **WHEN** a user calls graph expansion without providing `queryContext`
- **THEN** the system SHALL traverse edges in standard BFS order (no embedding comparisons)
- **AND** behavior SHALL be identical to the current implementation

#### Scenario: Edges with null embeddings are included at lowest priority

- **WHEN** expanding with query context and some edges have `embedding IS NULL`
- **THEN** edges with null embeddings SHALL be assigned a similarity score of 0.0
- **AND** they SHALL be traversed after all edges with computed similarity scores

#### Scenario: Query-aware expansion respects existing limits

- **WHEN** expanding with query context, maxDepth=2, and maxNodes=100
- **THEN** the system SHALL respect maxDepth and maxNodes limits as before
- **AND** prioritization SHALL only affect the order of edge traversal within each BFS level, not the depth or node count limits

### Requirement: Query context parameter on expansion API

The system SHALL accept an optional `queryContext` string parameter on the graph expansion endpoint to enable query-aware edge prioritization.

#### Scenario: API accepts queryContext parameter

- **WHEN** a client sends a graph expansion request with `{ "queryContext": "search terms" }`
- **THEN** the system SHALL use the provided string to embed and score edges
- **AND** the response format SHALL remain unchanged (same node/edge arrays)

#### Scenario: queryContext parameter is optional

- **WHEN** a client sends a graph expansion request without `queryContext`
- **THEN** the request SHALL be valid
- **AND** expansion SHALL use standard unranked BFS

#### Scenario: Empty queryContext treated as absent

- **WHEN** a client sends `{ "queryContext": "" }`
- **THEN** the system SHALL treat it as if queryContext was not provided
- **AND** standard BFS SHALL be used

### Requirement: Single embedding call for query-aware expansion

The system SHALL embed the query context string once and reuse the vector across all BFS levels, not re-embed per level or per edge.

#### Scenario: One embedding call regardless of graph depth

- **WHEN** expanding a graph with queryContext at maxDepth=3
- **THEN** the system SHALL make exactly one call to the embedding service for the query context
- **AND** the same vector SHALL be reused for scoring edges at all traversal levels

