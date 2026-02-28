# graph-search Specification

## Purpose
Specification for graph search functionality.
## Requirements
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

### Requirement: LLM context includes relationship triplets for richer answers

The system SHALL include matched relationship triplets in LLM context alongside matched graph objects to provide connection information.

**Modified from:** LLM context contains only matched object properties  
**Modified to:** LLM context contains matched objects AND triplet text of relevant relationships

#### Scenario: LLM receives relationship context

- **WHEN** search returns 5 objects and 3 relationships
- **THEN** LLM prompt includes both object details and relationship triplets (e.g., "Elon Musk founded Tesla")

#### Scenario: Relationship context format in prompt

- **WHEN** constructing LLM context with relationships
- **THEN** system formats relationships as: "Relationship: {triplet_text}" or similar clear structure

### Requirement: Search response format remains backward compatible

The system SHALL maintain backward compatibility for search response structure while adding optional relationship results and transitioning to new ID field names.

**Modified from:** Response contains only `objects: []` array  
**Modified to:** Response contains `objects: []` AND optional `relationships: []` array, with objects using `version_id`/`entity_id` fields alongside deprecated `id`/`canonical_id` during transition

#### Scenario: Existing clients ignore relationship results

- **WHEN** old client receives search response with `relationships` array
- **THEN** client can safely ignore new field and use `objects` array as before

#### Scenario: New clients consume relationship results

- **WHEN** new client receives search response
- **THEN** client can access both `objects` and `relationships` arrays for enhanced display

#### Scenario: Search response objects include new ID field names

- **WHEN** client receives search response during the deprecation period
- **THEN** each object in the `objects` array SHALL include both `version_id`/`entity_id` and deprecated `id`/`canonical_id` fields

#### Scenario: Search response objects use only new names after deprecation

- **WHEN** client receives search response after the deprecation period ends (major version bump)
- **THEN** each object SHALL include only `version_id` and `entity_id`, with `id`/`canonical_id` removed

### Requirement: FTSSearch SDK response uses VersionID and EntityID

The SDK SHALL expose FTS search result objects using the `VersionID` and `EntityID` field names, consistent with the GraphObject struct rename.

#### Scenario: FTSSearch result objects use new field names

- **WHEN** SDK consumer calls `FTSSearch` and iterates over results
- **THEN** each result object SHALL have `VersionID` and `EntityID` fields populated

#### Scenario: FTSSearch deprecated fields accessible during transition

- **WHEN** SDK consumer accesses `result.ID` or `result.CanonicalID` during the deprecation period
- **THEN** the values SHALL be identical to `result.VersionID` and `result.EntityID` respectively

### Requirement: HybridSearch SDK response uses VersionID and EntityID

The SDK SHALL expose hybrid search result objects using the `VersionID` and `EntityID` field names, consistent with the GraphObject struct rename.

#### Scenario: HybridSearch result objects use new field names

- **WHEN** SDK consumer calls `HybridSearch` and iterates over results
- **THEN** each result object SHALL have `VersionID` and `EntityID` fields populated

#### Scenario: HybridSearch deprecated fields accessible during transition

- **WHEN** SDK consumer accesses `result.ID` or `result.CanonicalID` during the deprecation period
- **THEN** the values SHALL be identical to `result.VersionID` and `result.EntityID` respectively

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

