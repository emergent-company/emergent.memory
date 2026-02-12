## MODIFIED Requirements

### Requirement: Hybrid search includes relationship embeddings alongside object embeddings

The system SHALL extend existing hybrid search to query both graph object embeddings and graph relationship embeddings, merging results via Reciprocal Rank Fusion.

**Modified from:** Search queries only `kb.graph_objects.embedding_vec`  
**Modified to:** Search queries both `kb.graph_objects.embedding_vec` AND `kb.graph_relationships.embedding`

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

The system SHALL maintain backward compatibility for search response structure while adding optional relationship results.

**Modified from:** Response contains only `objects: []` array  
**Modified to:** Response contains `objects: []` AND optional `relationships: []` array

#### Scenario: Existing clients ignore relationship results

- **WHEN** old client receives search response with `relationships` array
- **THEN** client can safely ignore new field and use `objects` array as before

#### Scenario: New clients consume relationship results

- **WHEN** new client receives search response
- **THEN** client can access both `objects` and `relationships` arrays for enhanced display
