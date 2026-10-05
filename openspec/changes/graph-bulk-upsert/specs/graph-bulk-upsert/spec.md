# graph-bulk-upsert Specification

## Purpose

Provide first-class, keyed bulk upsert paths for graph objects and relationships,
plus a batched multi-key lookup on object search, so blueprint/seed reconciliation
scales with batch count rather than per-object round trips while preserving
per-item error isolation and single-path project scoping.

## ADDED Requirements

### Requirement: Bulk upsert objects by (type, key)

The API SHALL accept a bulk request of object upserts where each item is resolved
by the pair `(type, key)` with create-or-replace semantics: when no non-deleted
head object with that `(type, key)` exists in the project, a new object is
inserted; otherwise the existing head is matched and updated. The `key` field
SHALL be required on every item.

#### Scenario: New object inserted

- **WHEN** a client sends `POST /api/graph/objects/bulk-upsert` with an item
  whose `(type, key)` does not match any existing object in the project
- **THEN** a new object is created and the item's result carries
  `success: true` and `created: true` with the created object

#### Scenario: Existing object updated in place

- **WHEN** a client sends `POST /api/graph/objects/bulk-upsert` with an item
  whose `(type, key)` already matches an existing non-deleted head object
- **THEN** that existing object is updated (not duplicated) and the item's
  result carries `success: true` and `created: false` with the updated object

#### Scenario: Item missing a key

- **WHEN** a client sends `POST /api/graph/objects/bulk-upsert` with an item
  that has no `key`
- **THEN** that item's result reports `success: false` with an error and no
  object is created

### Requirement: Bulk upsert response shape and created flag

The bulk object upsert response SHALL be
`{"success","failed","results":[{"index","success","created","object?","error?"}]}`,
where `success` and `failed` count the outcomes, `index` is the item's position
in the request, and `created` is `true` only when a new object was inserted.

#### Scenario: Result mirrors request order

- **WHEN** a client sends a bulk-upsert request with multiple items
- **THEN** the response returns one result per item, each carrying the item's
  `index` and its own `success`, `created`, and `object`/`error` fields

### Requirement: Bulk upsert enforces a 100-item cap

The bulk object upsert endpoint SHALL enforce a maximum of 100 items per request,
independent of any general server batch cap.

#### Scenario: Over-cap request rejected

- **WHEN** a client sends `POST /api/graph/objects/bulk-upsert` with more than
  100 items
- **THEN** the request is rejected with an error and no items are processed

#### Scenario: At-cap request accepted

- **WHEN** a client sends `POST /api/graph/objects/bulk-upsert` with exactly 100
  items
- **THEN** the request is processed and a result is returned per item

### Requirement: Per-item error isolation in bulk upsert

A failure on one item in a bulk upsert SHALL NOT fail the batch: each item is
processed independently and its success or error is reported per item, so one bad
item leaves the remaining items applied.

#### Scenario: One bad item does not fail the batch

- **WHEN** a client sends a bulk-upsert request where one item is invalid and the
  others are valid
- **THEN** the invalid item's result reports `success: false` with an error and
  the valid items are still applied with `success: true`

### Requirement: Bulk upsert project scoping and authorization

The bulk object upsert SHALL be scoped and authorized identically to the
single-object write paths: the caller's project resolves the `(type, key)` match,
and the same authentication/project-membership guards apply.

#### Scenario: Bulk upsert scoped to the project

- **WHEN** an authorized caller sends a bulk-upsert request
- **THEN** existing-object matches are resolved only within the caller's project,
  so a same `(type, key)` in another project is not touched

#### Scenario: Unauthorized caller rejected

- **WHEN** an unauthenticated or non-member caller sends
  `POST /api/graph/objects/bulk-upsert`
- **THEN** the request is rejected with an authorization error and no objects are
  created or updated

### Requirement: Bulk upsert relationships keyed by (type, src, dst)

The API SHALL accept a bulk request of relationship upserts where each item is
resolved by the pair `(type, src, dst)` with idempotent create-or-update
semantics, scoped to the caller's project.

#### Scenario: New relationship created

- **WHEN** a client sends `POST /api/graph/relationships/bulk-upsert` with an
  item whose `(type, src, dst)` does not match an existing relationship
- **THEN** a new relationship is created and the item's result reports
  `success: true` and `created: true`

#### Scenario: Existing relationship updated idempotently

- **WHEN** a client re-sends a bulk relationship upsert for a `(type, src, dst)`
  that already exists
- **THEN** the existing relationship is returned/updated rather than duplicated
  and the item's result reports `created: false`

#### Scenario: Relationship per-item isolation

- **WHEN** a bulk relationship upsert contains one invalid item alongside valid
  items
- **THEN** the invalid item's result reports `success: false` with an error and
  the valid items are still applied

### Requirement: Multi-key lookup on object search

`GET /api/graph/objects/search` SHALL accept a `keys` query parameter holding a
comma-separated list of keys, applied as a `key IN (...)` filter alongside the
existing single `key` filter.

#### Scenario: Batched key lookup

- **WHEN** a client sends `GET /api/graph/objects/search?keys=a,b,c`
- **THEN** the API returns the head objects whose `key` is one of `a`, `b`, or
  `c` in the caller's project

#### Scenario: Single key filter unaffected

- **WHEN** a client sends `GET /api/graph/objects/search?key=a`
- **THEN** the API continues to filter by the single `key`, unchanged
