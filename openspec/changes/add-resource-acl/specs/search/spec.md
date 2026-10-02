## ADDED Requirements

### Requirement: Search filters by authorization, not just project

Every search leg (graph, text/chunk, relationship) SHALL filter by project id **and** by per-resource authorization, so that no search SHALL return a resource the caller is not authorized to read. Authorization SHALL be applied as a query filter, not as a post-hoc in-memory drop on an already-fetched candidate set.

#### Scenario: Unauthorized resource never returned

- **WHEN** a caller runs a search in a project where a resource has been denied to them
- **THEN** that resource SHALL NOT appear in any search leg's results

#### Scenario: Filter applied at query time

- **WHEN** a search leg runs
- **THEN** the authorization predicate SHALL be part of the same query that fetches candidates, so unauthorized resources are never fetched, scored, or counted

#### Scenario: Project members see their resources by default

- **WHEN** a caller is a project member and no explicit ACL entry denies them
- **THEN** they SHALL see their project's resources as today (backfill preserves current behaviour)
