# retrieval-trace-persistence Specification

## Purpose
Persists an addressable record of every search — the raw query, applied filters, candidate set, per-result lexical/vector/fused scores, and the selected node/passage IDs — under a stable trace ID. Traces are bounded (capped candidate count plus configurable retention) and can reconstruct the exact ordered result packet later without re-running embedding or lexical scoring.

## Requirements

### Requirement: Retrieval traces are persisted with stable IDs
Every search operation SHALL persist a retrieval trace containing: the raw query, applied filters, the candidate set, per-result scores (lexical, vector, fused), the selected node/passage IDs, and a stable, addressable trace ID. The trace MUST be retrievable later by that ID.

#### Scenario: Search persists an addressable trace
- **WHEN** a search completes
- **THEN** a trace row is written with a stable trace ID
- **AND** the response includes the trace ID
- **AND** the trace can be fetched by ID via an API or internal lookup

#### Scenario: Trace captures selection, not just candidates
- **WHEN** a search returns N final results from a larger candidate set
- **THEN** the trace MUST record which candidates were selected and which were discarded, with their scores

### Requirement: Trace records are bounded and configurable
The trace SHALL be bounded: only a configurable number of candidates and only the top-N scored fields are persisted. Trace retention SHALL be configurable (default TTL) to prevent unbounded growth.

#### Scenario: Trace size bounded under load
- **WHEN** a search produces thousands of candidates
- **THEN** the persisted trace MUST cap stored candidates at a configured maximum (e.g., 200)

#### Scenario: Traces expire
- **WHEN** the configured trace retention period elapses
- **THEN** expired trace rows SHALL be removed by a cleanup job

### Requirement: Exact-packet reproducibility
Given a persisted trace ID, the system MUST be able to reconstruct the exact ordered result list (node/passage IDs) that was returned to the caller at the time of the search, without re-running the embedding or lexical scoring.

#### Scenario: Reconstruct result packet from trace
- **WHEN** a trace ID is supplied to the reconstruction lookup
- **THEN** the exact ordered list of selected IDs from the original search MUST be returned
