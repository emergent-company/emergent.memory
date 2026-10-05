# Database resilience Specification

## ADDED Requirements

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
