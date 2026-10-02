## Purpose

Give unified-search callers an explicit switch to skip the relationship-vector search leg, so entity/text-only searches do not pay for a relationship embedding query they do not need. The MCP `search-hybrid` tool never surfaces relationship candidates, so it force-skips the leg; the REST `/api/search` path keeps the legacy behaviour (a `nil` field runs the leg when the result types allow it).

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

### Requirement: MCP search-hybrid always skips the relationship leg

The MCP `search-hybrid` tool SHALL always set `includeRelationships: false` on the unified-search request it issues, so the relationship-vector leg is never run for MCP consumers. This is a pure latency optimisation: `mapUnifiedToSearchResponse` only maps graph and text items, so relationship candidates produced by the leg were never surfaced to MCP clients.

#### Scenario: MCP search-hybrid skips the relationship leg

- **WHEN** a client calls the MCP `search-hybrid` tool
- **THEN** the underlying unified search is issued with `includeRelationships: false`
- **AND** no relationship candidates are produced or surfaced

#### Scenario: REST search keeps legacy behaviour

- **WHEN** a client calls the REST `/api/search` endpoint without an `includeRelationships` field
- **THEN** the relationship leg retains its legacy behaviour (runs when the result types allow it)
