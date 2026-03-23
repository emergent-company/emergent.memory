## ADDED Requirements

### Requirement: Blueprint source is recorded when schema is installed from a blueprint
When a schema is created via `memory blueprints`, the system SHALL persist the blueprint source identifier (file path or remote URL) in `kb.graph_schemas.blueprint_source`. When created via `memory schemas install` or the API directly without a source, `blueprint_source` SHALL be NULL.

#### Scenario: Blueprint install records source
- **WHEN** a schema is created via `POST /api/schemas` with `blueprint_source` set in the request body
- **THEN** the schema record in `kb.graph_schemas` SHALL have `blueprint_source` equal to the provided value

#### Scenario: Direct install has no blueprint source
- **WHEN** a schema is created via `POST /api/schemas` without a `blueprint_source` field
- **THEN** the schema record SHALL have `blueprint_source` equal to NULL

#### Scenario: Blueprint applier sets source from SourceFile
- **WHEN** `memory blueprints <dir>` is run and a pack is created
- **THEN** the `CreatePackRequest` sent to the server SHALL include `blueprint_source` equal to the pack's `SourceFile` path

#### Scenario: Blueprint source is returned in schema list responses
- **WHEN** `GET /api/schemas/projects/:id/installed` or `GET /api/schemas/projects/:id/all` is called
- **THEN** each item in the response that was installed from a blueprint SHALL include a non-null `blueprintSource` field matching the stored value
