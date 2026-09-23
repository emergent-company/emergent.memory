## Purpose

Defines how `GET /api/graph/objects/search` reports or skips the exact number of
matching graph objects, and the `kb.graph_objects` maintenance that keeps that
count cheap, so callers that only need a page do not pay for a
visibility-map-degraded `COUNT(*)`.

## ADDED Requirements

### Requirement: The exact total is returned by default

The search endpoint SHALL compute and return an exact integer `total` when the
request does not opt out, including when the exact total is zero. The value MUST
be the exact number of matching objects, never an estimate.

#### Scenario: Default request returns an exact total

- **WHEN** objects match the request's filters and the request omits `include_total`
- **THEN** the response MUST contain a `total` field
- **AND** its value MUST equal the exact number of matching objects

#### Scenario: A zero total is reported, not omitted

- **WHEN** no objects match the request's filters and the request omits `include_total`
- **THEN** the response MUST contain `"total": 0`

### Requirement: The exact count can be skipped

When the request sets `include_total=false`, the endpoint MUST NOT execute the
exact `COUNT(*)` for that request and MUST omit the `total` field from the
response, so that "skipped" is distinguishable from a real zero.

#### Scenario: Opting out omits the total and skips the count

- **WHEN** a request sets `include_total=false`
- **THEN** the response MUST NOT contain a `total` field
- **AND** no exact count query MUST be issued for that request

#### Scenario: Opting out composes with filters and pagination

- **WHEN** a request sets `include_total=false` together with `type`, `label`,
  `cursor`, `limit` or `ids` filters
- **THEN** `items` and `next_cursor` MUST be returned exactly as they are when
  the total is requested

### Requirement: The default response shape stays backwards compatible

The change to skip the total MUST be additive: clients that do not send
`include_total` MUST keep receiving the same response fields with the same names
and types as before.

#### Scenario: Unchanged wire shape for existing clients

- **WHEN** a client sends no `include_total` parameter
- **THEN** the response MUST contain `items`, `next_cursor` and an integer
  `total` under the same JSON names as before this capability existed

#### Scenario: Only the literal false opts out

- **WHEN** a request sends `include_total=true`, `include_total=1` or any value
  other than the literal `false`
- **THEN** the endpoint MUST behave as if the parameter were absent and return
  the exact total

### Requirement: kb.graph_objects autovacuum keeps the visibility map fresh

`kb.graph_objects` SHALL carry per-table autovacuum settings aggressive enough
that ordinary churn on a large project cannot leave most pages without a
visibility-map bit, so the index-only `COUNT(*)` and list scans do not fall back
to the heap.

#### Scenario: The migration sets and resets the reloptions

- **WHEN** migration `00176_objects_count_optimizations.sql` is applied
- **THEN** `kb.graph_objects` MUST carry `autovacuum_vacuum_scale_factor=0.02`,
  `autovacuum_vacuum_threshold=500`, and matching analyze settings
- **AND** its Down migration MUST reset those options so the server defaults
  apply again

#### Scenario: A fresh visibility map yields a heap-free index-only count

- **WHEN** the visibility map for `kb.graph_objects` is fully set
- **THEN** the `COUNT(*)` for a dominant project MUST report `Heap Fetches: 0`
- **AND** it MUST complete in tens of milliseconds at 120k HEAD rows in one
  project, rather than the seconds it takes while the map is stale

#### Scenario: No index is added by this capability

- **WHEN** migration `00176` is applied
- **THEN** no index on `kb.graph_objects` MUST be created or dropped
