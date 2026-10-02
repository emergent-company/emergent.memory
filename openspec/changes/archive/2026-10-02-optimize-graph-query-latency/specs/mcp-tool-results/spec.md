## Purpose

Make the expensive relationship-type aggregation behind the `entity-type-list` MCP tool opt-in, so the tool stays fast for type/count/schema questions and only pays for relationship-type counts when a caller explicitly asks for them.

## ADDED Requirements

### Requirement: entity-type-list relationship types are opt-in

The `entity-type-list` MCP tool SHALL NOT compute relationship type counts unless the caller passes `include_relationships: true`. By default (`include_relationships` omitted or false) the tool SHALL return entity types with instance counts and an empty `relationships` list, and SHALL NOT run the relationship-type aggregation query. When `include_relationships: true`, the tool SHALL additionally return relationship types as `(type, from_type, to_type, count)` rows, computed by a project-scoped, head-only query that filters `gr.supersedes_id IS NULL`, `src.project_id`, and `dst.project_id` (so superseded/legacy cross-project rows are not counted). The performance win comes from skipping this aggregation on the default path, not from index selection.

#### Scenario: Default call omits relationship types

- **WHEN** a client calls `entity-type-list` without `include_relationships`
- **THEN** the result contains entity types with counts and an empty `relationships` list
- **AND** the relationship-type aggregation query is not executed

#### Scenario: Opt-in call returns relationship types

- **WHEN** a client calls `entity-type-list` with `include_relationships: true`
- **THEN** the result contains relationship types as `(type, from_type, to_type, count)` rows

#### Scenario: Relationship query is project-scoped and head-only

- **WHEN** the relationship-type aggregation runs
- **THEN** its SQL filters `gr.supersedes_id IS NULL`, `src.project_id`, and `dst.project_id`, and its arguments are ordered to match the SQL placeholders
