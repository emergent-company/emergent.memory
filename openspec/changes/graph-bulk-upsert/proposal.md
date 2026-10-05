## Why

Blueprint and seed-pack reconciliation is O(N) round trips. The CLI seeder
pre-checks every seed object with a per-key `GET /api/graph/objects/search`, then
bulk-creates only the new records and upserts every already-existing record
individually. A pack with thousands of seed objects pays thousands of HTTP round
trips on every re-apply, and the graph API has no idempotent, keyed bulk-write
path to collapse that into a handful of requests. As seed packs grow, re-applying
a blueprint dominates runtime with per-object latency rather than doing real work.

## What Changes

- Add `POST /api/graph/objects/bulk-upsert` (≤100 items/request): each item is
  resolved by `(type, key)` and create-or-replaced; `key` is required per item.
  Response is `{"success","failed","results":[{"index","success","created","object?","error?"}]}`
  where `created` is true only when a new object was inserted and false when an
  existing head was matched/updated.
- Add `POST /api/graph/relationships/bulk-upsert` (≤100 items/request): each item
  is an upsert keyed by `(type, src, dst)`; idempotent, per-item isolated,
  project scoped. Response mirrors the objects shape with a `relationship` per
  result.
- Add batched key lookup: `GET /api/graph/objects/search` gains a `keys` query
  param (comma-separated) mapping to a `key IN (...)` filter alongside the
  existing single `key` filter.
- Refactor the CLI `memory blueprints` seeder reconciliation to use the batched
  multi-key lookup, the objects bulk-upsert, and the relationships bulk-upsert,
  preserving per-item error reporting, counters, and `assignee`/`status`
  forwarding.

## Capabilities

### New Capabilities

- `graph-bulk-upsert`: first-class, keyed bulk upsert paths for graph objects and
  relationships plus a batched multi-key lookup on object search, with per-item
  error isolation and single-path-equivalent project scoping/authz.

### Modified Capabilities

- `blueprint-api`: the blueprint/seed apply path now resolves all seed keys in one
  batched lookup and applies objects and relationships through the bulk-upsert
  endpoints; seed objects still carry `assignee`/`status`, now forwarded through
  the bulk path, and reconciliation cost scales with batch count rather than
  object count.

## Impact

- **Server** (`apps/server/domain/graph/`): new `BulkUpsertObjects` and
  `BulkUpsertRelationships` DTOs/handlers/services/routes; repository gains
  `FindHeadsByTypeAndKeys` and `ListParams.Keys`; the search handler parses the
  new `keys` query param into `key IN (...)`.
- **SDK** (`apps/server/pkg/sdk/graph/`): `BulkUpsertObjects` and
  `BulkUpsertRelationships` methods plus `ListObjectsOptions.Keys` (the latter
  already emits a `keys` query param), with the server side now implementing the
  matching endpoints and filter.
- **CLI** (`apps/cli/internal/blueprints/`): the seeder's `applyObjects` and
  `applyRelationships` switch from per-key lookups and per-record upserts to the
  batched lookup and bulk-upsert calls, keeping the existing `SeedResult`
  counters and dry-run behaviour.
- **Tests**: server DB tests for the new repository/service paths, SDK client
  tests for the bulk-upsert calls and `keys` param, CLI seeder tests for batched
  reconciliation, plus build/lint verification. No breaking changes: existing
  single-object and bulk endpoints are unchanged.
