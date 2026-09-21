## Purpose

Keeps the web Objects list and detail pages fast on projects with large object graphs by eliminating full-table scans and serial sub-resource waterfalls, without changing any page output, routes, or client contracts.

## ADDED Requirements

### Requirement: Indexed head-object listing

The objects list query (`GET /api/graph/objects/search`) SHALL be served by an index that matches its head/main-branch/not-deleted predicates and its `created_at DESC, id DESC` ordering.

#### Scenario: No full-table scan

- **WHEN** the objects list query runs for a project with a large object graph
- **THEN** the database uses an index scan over the partial index instead of a sequential scan of `kb.graph_objects`

#### Scenario: Planner stats refreshed

- **WHEN** the migration completes
- **THEN** `kb.graph_objects` statistics are refreshed so the planner selects the new index and planning time does not regress

### Requirement: Concurrent detail-page sub-resources

The object detail handler SHALL fetch its independent sub-resources (compiled types, edges, similar objects, label suggestions) concurrently rather than sequentially.

#### Scenario: Independent fetches overlap

- **WHEN** the object detail page renders
- **THEN** its independent backend calls are issued concurrently and the page latency is bounded by the slowest call, not their sum

### Requirement: Cached label suggestions

The project's distinct object labels for the label-autocomplete hint SHALL be served from a short-lived cache instead of re-running the object list query on every render.

#### Scenario: Cache hit

- **WHEN** the label suggestions were fetched for a project within the cache TTL
- **THEN** the cached labels are returned without an additional object-list query

#### Scenario: Cache miss and refresh

- **WHEN** no cached labels exist for the project or the entry is expired
- **THEN** the labels are fetched, cached with a TTL, and returned
