## MODIFIED Requirements

### Requirement: Retrieval traces are persisted with stable IDs

Every search operation SHALL persist a retrieval trace containing: the raw query, applied filters, the candidate set, per-result scores (lexical, vector, fused), the selected node/passage IDs, and a stable, addressable trace ID. The trace MUST be retrievable later by that ID. In addition, the trace SHALL record the authenticated user who issued the search and a stable query key (a deterministic normalized form of the query) when those are known, so the trace can be correlated with answer feedback for retrieval-quality evaluation.

#### Scenario: Search persists an addressable trace

- **WHEN** a search completes
- **THEN** a trace row is written with a stable trace ID
- **AND** the response includes the trace ID
- **AND** the trace can be fetched by ID via an API or internal lookup

#### Scenario: Trace captures selection, not just candidates

- **WHEN** a search returns N final results from a larger candidate set
- **THEN** the trace MUST record which candidates were selected and which were discarded, with their scores

#### Scenario: Trace captures the issuing user

- **WHEN** a search completes in an authenticated context
- **THEN** the persisted trace records the issuing user's id
- **AND** a search issued without an authenticated user records a null user id

#### Scenario: Trace captures a stable query key

- **WHEN** a search completes
- **THEN** the persisted trace records a stable query key derived deterministically from the raw query
- **AND** the same query submitted twice yields the same query key

#### Scenario: Query history is readable

- **WHEN** a caller requests the search-query history for a project (optionally scoped to a user)
- **THEN** the stored traces for that project SHALL be returned in reverse chronological order without re-running embedding or lexical scoring
