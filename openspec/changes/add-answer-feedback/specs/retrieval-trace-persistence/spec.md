## MODIFIED Requirements

### Requirement: Retrieval traces are persisted with stable IDs

Every search operation SHALL persist a retrieval trace containing: the raw query, applied filters, the candidate set, per-result scores (lexical, vector, fused), the selected node/passage IDs, and a stable, addressable trace ID. The trace MUST be retrievable later by that ID. In addition, the trace SHALL record the authenticated user who issued the search when that is known, so the trace can be correlated with answer feedback for retrieval-quality evaluation.

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

## ADDED Requirements

### Requirement: Search-query history is readable

A caller SHALL be able to read the search-query history for a project (optionally scoped to a user) as a reverse-chronological list of the persisted retrieval traces, without re-running embedding or lexical scoring.

#### Scenario: Query history lists a project's traces

- **WHEN** a caller requests the search-query history for a project
- **THEN** the stored traces for that project SHALL be returned in reverse chronological order

#### Scenario: Query history is user-scopable

- **WHEN** a caller requests search-query history scoped to a user
- **THEN** only traces with a matching `user_id` SHALL be returned, and traces with a null user id SHALL be excluded
