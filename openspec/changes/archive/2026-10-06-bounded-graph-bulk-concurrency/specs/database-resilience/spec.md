# Database resilience Specification

## ADDED Requirements

### Requirement: Bounded parallel execution of bulk endpoint items

Bulk object create, upsert and update endpoints SHALL process items with a
bounded worker pool rather than serially. The pool size SHALL be configurable
via `GRAPH_BULK_CONCURRENCY` (default `8`) and SHALL be clamped to at least `1`
and to at most the connection-pool size minus one (`DB_MAX_OPEN_CONNS - 1`,
never below `1`), reserving one pooled connection for other traffic, so a bulk
request can never attempt to claim the whole pool. Each request index SHALL be
visited exactly once, so `results[i]` remains aligned with `items[i]` and
per-item error isolation is preserved. Concurrent items that target the same
object or the same `(type, key)` SHALL continue to serialize on the existing
per-identity advisory lock, preserving one HEAD per object and the
`supersedes_id` version chain.

#### Scenario: Independent items overlap

- **WHEN** a bulk request contains many items that target distinct objects
- **THEN** up to `GRAPH_BULK_CONCURRENCY` items SHALL run concurrently and the
  batch SHALL complete in less wall time than the serial path

#### Scenario: Concurrency never exceeds the pool

- **WHEN** `GRAPH_BULK_CONCURRENCY` is greater than `DB_MAX_OPEN_CONNS`
- **THEN** the effective worker count SHALL be clamped to `DB_MAX_OPEN_CONNS - 1`
  (or `1` when the pool itself holds a single connection), leaving one pooled
  connection available for other traffic

#### Scenario: Same-identity items stay linear

- **WHEN** two items in one bulk request target the same object or `(type, key)`
- **THEN** their versions SHALL form a linear chain with exactly one HEAD

#### Scenario: Results stay index-aligned

- **WHEN** a bulk request mixes succeeding and failing items
- **THEN** `results[i]` SHALL correspond to `items[i]`, and a failed item SHALL
  NOT affect the outcome of any other item
