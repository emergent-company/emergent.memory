## Purpose

Make the expensive relationship-type aggregation behind the `entity-type-list` MCP tool opt-in, so the tool stays fast for type/count/schema questions and only pays for relationship-type counts when a caller explicitly asks for them.

## ADDED Requirements

### Requirement: entity-type-list relationship types are opt-in

The `entity-type-list` MCP tool SHALL NOT compute relationship type counts unless the caller passes `include_relationships: true`. By default (`include_relationships` omitted or false) the tool SHALL return entity types with instance counts and an empty `relationships` list, and SHALL NOT run the relationship-type aggregation query. When `include_relationships: true`, the tool SHALL additionally return relationship types as `(type, from_type, to_type, count)` rows, computed by a project-scoped query that filters `supersedes_id IS NULL` so it can use the partial index `idx_graph_relationships_head_main`.

#### Scenario: Default call omits relationship types

- **WHEN** a client calls `entity-type-list` without `include_relationships`
- **THEN** the result contains entity types with counts and an empty `relationships` list
- **AND** the relationship-type aggregation query is not executed

#### Scenario: Opt-in call returns relationship types

- **WHEN** a client calls `entity-type-list` with `include_relationships: true`
- **THEN** the result contains relationship types as `(type, from_type, to_type, count)` rows

#### Scenario: Relationship query is project-scoped and index-friendly

- **WHEN** the relationship-type aggregation runs
- **THEN** its SQL filters `gr.supersedes_id IS NULL`, `src.project_id`, and `dst.project_id`, and its arguments are ordered to match the SQL placeholders

### Requirement: search-hybrid relationship candidates are opt-in

The `search-hybrid` MCP tool SHALL NOT include relationship (triple) candidates in its fused results unless the caller passes `include_relationships: true`. By default the tool runs faster, returning only entity/text candidates.

#### Scenario: Default search omits relationship candidates

- **WHEN** a client calls `search-hybrid` without `include_relationships`
- **THEN** the result contains entity/text candidates only, and the relationship-vector search leg is skipped

#### Scenario: Opt-in search includes relationship candidates

- **WHEN** a client calls `search-hybrid` with `include_relationships: true`
- **THEN** the result may include relationship (triple) candidates
