# database-resilience Specification

## Purpose
Define how the server establishes and bounds database connectivity — retrying startup connectivity with backoff instead of failing instantly, and applying a transaction-local statement timeout on bulk write endpoints so a runaway statement cannot starve the connection pool.

## Requirements

### Requirement: Bounded startup connectivity retry

The server SHALL verify database connectivity at startup by pinging the
connection pool with a bounded retry and exponential backoff before failing
startup. The number of attempts SHALL be configurable via
`DB_PING_MAX_ATTEMPTS` (default `10`); the initial backoff via
`DB_PING_INITIAL_BACKOFF` (default `250ms`); and the backoff ceiling via
`DB_PING_MAX_BACKOFF` (default `5s`). The pool SHALL NOT be recreated between
attempts, and the final error SHALL name the number of attempts and the last
ping failure.

#### Scenario: Transient outage is ridden out

- **WHEN** the database is unreachable for the first few ping attempts and
  becomes reachable before the attempt budget is spent
- **THEN** startup SHALL succeed without restarting the process

#### Scenario: Persistent outage still fails closed

- **WHEN** every ping attempt fails within the budget
- **THEN** startup SHALL fail with an error naming the attempt count and the
  last ping failure, and the pool SHALL be closed

#### Scenario: Attempts are clamped

- **WHEN** `DB_PING_MAX_ATTEMPTS` is less than `1`
- **THEN** the server SHALL make exactly one ping attempt

### Requirement: Per-statement timeout on bulk endpoints

Batch endpoints (`bulk`, `bulk-upsert`, `bulk-update`, and their relationship
variants) SHALL bound each statement executed for an item with PostgreSQL's
`statement_timeout`, applied transaction-locally via
`set_config('statement_timeout', …, true)` when the item transaction begins.
The bound SHALL be configurable via `DB_BULK_STATEMENT_TIMEOUT` (default `10m`);
a value of `0` SHALL disable the bound. Non-bulk transactions SHALL NOT receive
the setting, and the existing per-request bulk budget SHALL be unchanged.

#### Scenario: Runaway statement fails cleanly

- **WHEN** a bulk item statement exceeds `DB_BULK_STATEMENT_TIMEOUT`
- **THEN** that statement SHALL be cancelled by PostgreSQL and fail the item
  rather than holding the connection for the whole request budget

#### Scenario: Normal bulk op is unaffected

- **WHEN** a bulk request completes within its statement and request budgets
- **THEN** all items SHALL succeed exactly as before

#### Scenario: Ordinary transactions pay no extra round trip

- **WHEN** a transaction is started from a context without a configured bulk
  statement timeout
- **THEN** no `set_config('statement_timeout', …)` statement SHALL be issued

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
