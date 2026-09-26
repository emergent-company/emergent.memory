## MODIFIED Requirements

### Requirement: Read and search tools return slim entities by default

Read-oriented MCP tools (`search-hybrid`, `search-semantic`, `entity-query`, and similar) SHALL return slim entity records by default, exposing only fields useful to agents and UIs (`id`, `type`, `key`, `name`, `properties` as applicable) and SHALL NOT emit verbose internal fields (`canonical_id`, `project_id`, `version_id`, unnecessary `created_at`/`updated_at`, `score` inside the object, nested pagination blobs) in the default projection. Full records MAY be returned when the caller passes an explicit `verbose`/`include_meta` flag. When a search returns scores, `score` SHALL appear at the result-item level, not nested inside the entity object.

`entity-query` SHALL project the `properties` blob through a `field_strategy` input with the same `compact` / `minimal` / `full` values as `search-hybrid`. The type/pagination path SHALL default to `compact` (name only, no `properties`), so the wide JSONB column is not selected by default; `full` SHALL return the full `properties` map (or the requested `fields` subset). The `ids[]` fast-path SHALL default to `full` so the documented "fetch the full properties of a specific version" behaviour is preserved, and SHALL honour an explicit `field_strategy` override.

#### Scenario: entity-query defaults to compact
- **WHEN** a client calls `entity-query` with `type_name` and no `field_strategy`
- **THEN** each returned entity SHALL omit the `properties` map (or return it empty)
- **AND** the underlying query SHALL NOT select the full `properties` JSONB column

#### Scenario: entity-query ids[] returns full properties
- **WHEN** a client calls `entity-query` with `ids=[...]` and no `field_strategy`
- **THEN** each returned entity SHALL include its full `properties` map

#### Scenario: field_strategy full returns properties
- **WHEN** a client calls `entity-query` with `field_strategy: "full"`
- **THEN** each returned entity SHALL include its `properties` map (full, or the requested `fields` subset)

#### Scenario: Default search-hybrid response is slim
- **WHEN** a client calls `search-hybrid` without a verbose flag
- **THEN** each returned entity SHALL contain only the slim field set
- **THEN** the entity object SHALL NOT contain internal fields like `canonical_id` or `project_id`
- **THEN** any relevance `score` SHALL be a sibling of the entity object, not inside it

#### Scenario: Verbose search returns full records
- **WHEN** a client calls a read tool with `verbose: true` (or equivalent)
- **THEN** the result SHALL include the fuller entity records including internal identifiers and timestamps
