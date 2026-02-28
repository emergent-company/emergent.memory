# unified-search Specification

## Purpose
TBD - created by archiving change add-unified-hybrid-search-tests. Update Purpose after archive.
## Requirements
### Requirement: Unified Search Endpoint

The system SHALL provide a unified search endpoint that executes both graph object search and document chunk search in parallel and returns combined results with configurable fusion strategies.

**Implementation Status:** ✅ COMPLETED

**Files:**

- `apps/server-go/domain/search/service.go` - Service with parallel execution and fusion logic
- `apps/server-go/domain/search/repository.go` - Search queries

**Fusion Strategies Implemented:**

1. `weighted` - Score-based combination with configurable weights (default 0.5/0.5) and independent `relationshipWeight`
2. `rrf` - Reciprocal Rank Fusion (k=60, rank-based)
3. `interleave` - Alternates between graph, text, and relationship results
4. `graph_first` - All graph results, then relationship results, then text results
5. `text_first` - All text results, then relationship results, then graph results

#### Scenario: Execute unified search with single embedding call

- **GIVEN** a user query "authentication patterns"
- **WHEN** the user calls `POST /search/unified` with the query
- **THEN** the system SHALL embed the query string exactly once via the embedding service
- **AND** the system SHALL pass the pre-computed embedding vector to all three parallel search goroutines (graph, text, relationship)
- **AND** no search goroutine SHALL independently call the embedding service
- **AND** total embedding API calls for one unified search request SHALL be exactly 1

#### Scenario: Apply fusion strategy to combine results including relationships

- **GIVEN** graph results, text results, and relationship results from parallel searches
- **WHEN** unified search applies any fusion strategy
- **THEN** all five strategies SHALL include relationship results in their output
- **AND** `weighted` strategy SHALL apply `relationshipWeight` to relationship result scores (defaulting to `graphWeight` if not specified)
- **AND** `rrf` strategy SHALL include relationship results in the RRF ranking
- **AND** `interleave` strategy SHALL alternate between graph, text, and relationship results
- **AND** `graph_first` strategy SHALL place relationship results after graph results and before text results
- **AND** `text_first` strategy SHALL place relationship results after text results and before graph results
- **AND** combined results SHALL be limited by `limit` parameter

#### Scenario: Independent relationship weight in weighted fusion

- **GIVEN** a unified search request with `graphWeight: 0.6`, `textWeight: 0.3`, `relationshipWeight: 0.1`
- **WHEN** the weighted fusion strategy is applied
- **THEN** graph result scores SHALL be multiplied by 0.6
- **AND** text result scores SHALL be multiplied by 0.3
- **AND** relationship result scores SHALL be multiplied by 0.1
- **AND** weights SHALL be normalized to sum to 1.0

#### Scenario: Backward compatible when relationshipWeight is omitted

- **GIVEN** a unified search request with only `graphWeight` and `textWeight` specified
- **WHEN** the weighted fusion strategy is applied
- **THEN** `relationshipWeight` SHALL default to the value of `graphWeight`
- **AND** all three weights SHALL be normalized to sum to 1.0

#### Scenario: Handle empty results gracefully

- **GIVEN** a query that matches no graph objects, text chunks, or relationships
- **WHEN** unified search is executed
- **THEN** the response SHALL return empty array for `results`
- **AND** metadata SHALL indicate zero `totalResults`
- **AND** no error SHALL be thrown
- **AND** HTTP status SHALL be 200

### Requirement: Relationship Expansion

Graph objects in unified search results SHALL include expanded relationship information when requested.

**Implementation Status:** ✅ COMPLETED

**Key Features:**

- Calls `GraphService.expand()` for relationship traversal
- Supports depth 0-3 (default 1)
- Supports directional filtering: in, out, both (default both)
- Limits neighbors per result (max 20, default 5)
- Returns relationship metadata (type, source, target, properties, direction)

#### Scenario: Expand outgoing relationships

- **GIVEN** a graph object with outgoing relationships (e.g., Decision depends_on Requirement)
- **WHEN** unified search returns that object with `includeRelationships: true`
- **THEN** the object's `relationships` array SHALL include outgoing relationships
- **AND** each relationship SHALL include target object fields (title, type, key)
- **AND** relationship metadata SHALL include relationship type and direction

#### Scenario: Limit relationship expansion depth

- **GIVEN** graph objects with multi-hop relationship chains
- **WHEN** unified search includes relationships
- **THEN** only direct (1-hop) relationships SHALL be included
- **AND** transitive relationships SHALL NOT be expanded automatically
- **AND** clients MAY call `/graph/traverse` for deeper expansion

#### Scenario: Filter relationships by type

- **GIVEN** request parameter `relationshipTypes: ["depends_on", "implements"]`
- **WHEN** unified search expands relationships
- **THEN** only relationships matching the specified types SHALL be included
- **AND** other relationship types SHALL be excluded

### Requirement: Result Scoring and Ranking

Unified search results SHALL preserve individual scoring from each search mode while providing combined metadata.

#### Scenario: Preserve search scores

- **GIVEN** graph search assigns scores to objects based on hybrid search
- **AND** text search assigns scores to chunks based on hybrid search
- **WHEN** unified search returns results
- **THEN** each graph object SHALL include its hybrid search score (0.0-1.0)
- **AND** each text chunk SHALL include its hybrid search score (0.0-1.0)
- **AND** scores SHALL NOT be normalized across result types

#### Scenario: Include search metadata

- **GIVEN** graph search and text search produce metadata (mode, lexical_score, vector_score)
- **WHEN** unified search returns results
- **THEN** `graphResults.meta` SHALL include graph search metadata
- **AND** `textResults.meta` SHALL include text search metadata
- **AND** top-level `meta` SHALL include combined query_time_ms and total_results

### Requirement: Authentication and Authorization

Unified search SHALL enforce the same scope-based authorization as individual search endpoints.

#### Scenario: Require search:read scope

- **GIVEN** a user without `search:read` scope
- **WHEN** the user calls `POST /search/unified`
- **THEN** the request SHALL be rejected with 403 Forbidden
- **AND** the response SHALL include missing scope information

#### Scenario: Apply RLS tenant context

- **GIVEN** an authenticated user with org_id and project_id context
- **WHEN** unified search executes
- **THEN** graph search SHALL filter objects by tenant context (RLS)
- **AND** text search SHALL filter chunks by tenant context (RLS)
- **AND** results SHALL only include data accessible to the user

### Requirement: Performance and Error Handling

Unified search SHALL optimize for performance and handle partial failures gracefully.

#### Scenario: Execute searches in parallel

- **GIVEN** unified search is called
- **WHEN** both graph and text searches are executed
- **THEN** searches SHALL run concurrently (Promise.all)
- **AND** total query time SHALL not exceed sum of individual search times
- **AND** query_time_ms SHALL reflect wall-clock time, not sum

#### Scenario: Handle partial search failures

- **GIVEN** text search succeeds but graph search fails (e.g., timeout)
- **WHEN** unified search is executed
- **THEN** the response SHALL include successful text results
- **AND** `graphResults.objects` SHALL be empty
- **AND** metadata SHALL include a warning about graph search failure
- **AND** HTTP status SHALL be 200 (partial success)

#### Scenario: Apply performance timeouts

- **GIVEN** unified search is configured with a 5-second timeout
- **WHEN** either search exceeds the timeout
- **THEN** that search SHALL be cancelled
- **AND** the response SHALL include results from the faster search
- **AND** metadata SHALL include a timeout warning

### Requirement: Request and Response Schema

Unified search SHALL accept structured request parameters and return a well-defined JSON schema.

#### Scenario: Validate request parameters

- **GIVEN** a request with invalid parameters (e.g., graphLimit: -1)
- **WHEN** the request is processed
- **THEN** validation SHALL fail before executing searches
- **AND** the response SHALL be 400 Bad Request
- **AND** the error SHALL include parameter validation details

#### Scenario: Return consistent response structure

- **GIVEN** any valid unified search request
- **WHEN** the search completes successfully
- **THEN** the response SHALL always include `query`, `graphResults`, `textResults`, and `meta` fields
- **AND** null fields SHALL be represented as empty arrays or null values
- **AND** the schema SHALL match OpenAPI specification exactly

