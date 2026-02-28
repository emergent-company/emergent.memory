# relationship-search Specification

## Purpose
Specification for relationship search functionality.
## Requirements
### Requirement: Vector similarity search across graph relationships

The system SHALL support semantic search across graph relationship embeddings using cosine similarity with improved recall via increased IVFFlat probes, returning relationships ranked by relevance to the search query.

#### Scenario: Search finds relevant relationships with improved recall

- **WHEN** user searches for "company founders" with query vector
- **THEN** system returns relationships of type FOUNDED_BY ranked by vector similarity to query
- **AND** the query SHALL set `ivfflat.probes = 10` before executing the vector search to scan 10 of 100 index lists

#### Scenario: Search filters null embeddings

- **WHEN** executing relationship vector search
- **THEN** system excludes relationships where `embedding IS NULL` from results

#### Scenario: Search uses ivfflat index with increased probes

- **WHEN** executing vector similarity query on relationships table
- **THEN** the system SHALL set `SET LOCAL ivfflat.probes = 10` within the transaction before executing the query
- **AND** query plan SHALL use `idx_graph_relationships_embedding` ivfflat index

#### Scenario: Pre-computed embedding vector accepted

- **WHEN** unified search passes a pre-computed query embedding vector to relationship search
- **THEN** relationship search SHALL use the provided vector directly
- **AND** relationship search SHALL NOT independently call the embedding service

### Requirement: Reciprocal Rank Fusion merging of node and edge results

The system SHALL merge graph object search results and relationship search results using Reciprocal Rank Fusion (RRF) algorithm with k=60 to produce unified ranked output.

#### Scenario: Successful RRF merging with both result types

- **WHEN** object search returns 10 results and relationship search returns 8 results
- **THEN** system merges using RRF formula: score = 1/(k + rank) where k=60, and returns top N combined

#### Scenario: RRF handles empty relationship results gracefully

- **WHEN** relationship search returns no results (no embeddings exist yet)
- **THEN** system returns only object search results without errors

#### Scenario: RRF handles empty object results gracefully

- **WHEN** object search returns no results
- **THEN** system returns only relationship search results without errors

### Requirement: Relationship triplet text in search response

The system SHALL include relationship triplet text in search responses to provide context for LLM prompts.

#### Scenario: Search response includes triplet text

- **WHEN** search returns a relationship result
- **THEN** response includes the triplet text field (e.g., "Elon Musk founded Tesla")

#### Scenario: Search response distinguishes nodes from edges

- **WHEN** search returns mixed results (objects and relationships)
- **THEN** response clearly indicates result type (object vs relationship) via type field or separate arrays

### Requirement: Search latency increase within acceptable bounds

The system SHALL maintain search performance with hybrid node+edge querying, keeping p95 latency increase under 100ms compared to object-only search.

#### Scenario: Hybrid search completes within latency budget

- **WHEN** executing search with both object and relationship queries
- **THEN** total search latency (including RRF merging) increases by less than 100ms at p95

#### Scenario: Parallel query execution

- **WHEN** executing hybrid search
- **THEN** system runs object search and relationship search in parallel (not sequential)

### Requirement: Configurable result limits for relationships

The system SHALL support limiting the number of relationship results retrieved before RRF merging to optimize performance.

#### Scenario: Relationship result limit applied

- **WHEN** search configuration specifies relationship_limit=50
- **THEN** system retrieves maximum 50 relationships from vector search before merging

#### Scenario: Default relationship limit

- **WHEN** no relationship_limit specified in search request
- **THEN** system uses default limit of 50 relationships

### Requirement: Search latency improvement from embedding deduplication

The system SHALL accept a pre-computed query embedding vector to avoid redundant embedding API calls, reducing search latency by approximately 200-400ms per unified search invocation.

#### Scenario: Relationship search uses pre-computed vector

- **WHEN** unified search orchestrator provides a pre-computed query embedding
- **THEN** relationship search SHALL use that vector for cosine similarity comparison
- **AND** no additional embedding API call SHALL be made

#### Scenario: Standalone relationship search embeds query

- **WHEN** relationship search is called directly (not via unified search) without a pre-computed vector
- **THEN** relationship search SHALL embed the query string itself

