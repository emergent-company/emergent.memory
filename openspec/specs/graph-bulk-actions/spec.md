# graph-bulk-actions Specification

## Purpose
Defines how filter-then-action bulk operations on graph objects bound the set of rows they
mutate, so a caller-supplied `limit` is honoured instead of silently ignored and the rows
that are affected are chosen deterministically.

## Requirements

### Requirement: Update-based bulk actions honour the limit

Every bulk action that mutates rows via an `UPDATE` SHALL cap the number of rows it affects
at the requested `limit`. PostgreSQL has no `UPDATE … LIMIT`, so the target set SHALL be
restricted with an `id IN (SELECT id … LIMIT n)` subselect.

#### Scenario: A capped update affects at most `limit` rows

- **WHEN** a bulk action (for example `update_status`, `soft_delete`, `merge_properties`,
  `replace_properties`, `set_labels`, `add_labels`, or `remove_labels`) matches 12 objects
  and is called with `limit = 5`
- **THEN** `matched` MUST report all 12 matched rows
- **AND** the update MUST affect exactly 5 rows
- **AND** the remaining 7 rows MUST be unchanged

#### Scenario: The cap restricts the mutation, not just the report

- **WHEN** a capped bulk update runs
- **THEN** the mutation MUST NOT extend to rows outside the `LIMIT`-bounded subselect,
  even though the outer `WHERE` matches them

### Requirement: Cap selection is deterministic

The `LIMIT`-bounded subselect SHALL order by `created_at ASC, id ASC`, where `id` is the
table primary key. The `id` secondary key MUST be applied inside the set the `LIMIT`
truncates, so the chosen subset is total and identical across runs.

#### Scenario: Tied sort keys still select the same rows

- **WHEN** many matched rows share the same `created_at` (for example from a bulk insert)
- **AND** the same capped bulk action is run repeatedly
- **THEN** the same rows MUST be affected every time
- **AND** the affected rows MUST be the oldest by `created_at`, ties broken by the smallest `id`

#### Scenario: Secondary key is inside the limited set

- **WHEN** the bounded subselect is built
- **THEN** the `ORDER BY created_at ASC, id ASC` and `LIMIT n` MUST be applied to the same
  subselect, so the tie-break influences which rows survive the `LIMIT`

### Requirement: Unset or zero limit means the default cap

A non-positive `limit` SHALL mean the configured default cap (`bulkActionDefaultLimit`, 1000),
applied before the action executes. It MUST NOT mean "unlimited".

#### Scenario: Unset limit uses the default

- **WHEN** a caller omits `limit` or passes `0`
- **THEN** the effective cap MUST be the default limit (1000)
- **AND** this MUST match the existing hard-delete behaviour

#### Scenario: Limit over the maximum is rejected

- **WHEN** a caller passes a `limit` greater than `bulkActionMaxLimit`
- **THEN** the service MUST return a validation error rather than clamping silently
