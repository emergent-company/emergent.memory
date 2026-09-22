## Purpose

Relationship vector search uses a distinct, lower `ivfflat.probes` default than chunk/graph-object search so the planner keeps selecting the ivfflat index on `kb.graph_relationships`.

## MODIFIED Requirements

### Requirement: Vector index probe count is configurable
The `ivfflat.probes` value used in chunk and graph-object search transactions SHALL be configurable via environment (`SEARCH_IVFFLAT_PROBES`), defaulting to 10. Relationship vector search uses a separate knob (see Relationship vector search probe count is configurable). The configured value MUST be applied per-query transaction.

#### Scenario: Probe count driven by environment
- **WHEN** the server starts with `SEARCH_IVFFLAT_PROBES=40`
- **THEN** chunk and graph-object search queries MUST execute with `SET LOCAL ivfflat.probes = 40`

#### Scenario: Omitted setting preserves default
- **WHEN** `SEARCH_IVFFLAT_PROBES` is unset
- **THEN** chunk and graph-object search queries MUST use the default of 10

## ADDED Requirements

### Requirement: Relationship vector search probe count is configurable
The `ivfflat.probes` value used in relationship vector search transactions SHALL be configurable via `SEARCH_RELATIONSHIP_IVFFLAT_PROBES`, defaulting to 5. The default is deliberately lower than the global default (10): at probes=10 the planner abandons the ivfflat index on `kb.graph_relationships` in favor of an optimistic parallel sequential scan, degrading relationship search from ~250ms to minutes. The configured value MUST be applied per-query transaction and MUST be clamped to a minimum of 1 (invalid, zero, or negative values fall back to the default).

#### Scenario: Relationship probe count driven by environment
- **WHEN** the server starts with `SEARCH_RELATIONSHIP_IVFFLAT_PROBES=3`
- **THEN** relationship search queries MUST execute with `SET LOCAL ivfflat.probes = 3`

#### Scenario: Relationship probe count omitted preserves default
- **WHEN** `SEARCH_RELATIONSHIP_IVFFLAT_PROBES` is unset
- **THEN** relationship search queries MUST use the default of 5, regardless of `SEARCH_IVFFLAT_PROBES`

#### Scenario: Invalid relationship probe count falls back to default
- **WHEN** `SEARCH_RELATIONSHIP_IVFFLAT_PROBES` is set to a non-numeric, zero, or negative value
- **THEN** relationship search queries MUST use the default of 5
