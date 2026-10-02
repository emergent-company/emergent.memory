## Purpose

Give unified-search callers an explicit switch to skip the relationship-vector search leg, so entity/text-only searches do not pay for a relationship embedding query they do not need.

## ADDED Requirements

### Requirement: Unified search can skip the relationship leg

`UnifiedSearchRequest` SHALL accept an optional `includeRelationships` boolean. When the field is `false`, `runParallelSearches` SHALL skip the relationship-vector search goroutine (no relationship candidates are produced). When the field is `nil`, the relationship leg SHALL retain its existing behaviour (it runs when the requested `ResultTypes` allow it). When `true`, the relationship leg runs as before.

#### Scenario: False skips the relationship leg

- **WHEN** a unified search runs with `includeRelationships: false` and result types that would otherwise include relationships
- **THEN** no relationship candidates are produced and the relationship search is not executed

#### Scenario: Nil preserves existing behaviour

- **WHEN** a unified search runs without an `includeRelationships` field
- **THEN** the relationship leg behaves as before this change (runs when the result types allow it)

#### Scenario: True runs the relationship leg

- **WHEN** a unified search runs with `includeRelationships: true`
- **THEN** the relationship leg runs and relationship candidates may appear in results
