## MODIFIED Requirements

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
