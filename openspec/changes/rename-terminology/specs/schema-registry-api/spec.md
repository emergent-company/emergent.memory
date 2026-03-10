## ADDED Requirements

### Requirement: Schema Registry API at /api/schema-registry

The system SHALL expose all project type catalog endpoints under the `/api/schema-registry` path prefix. The Go package SHALL be named `schemaregistry`, the fx module SHALL be named `schemaregistry`, and the primary entity SHALL be named `ProjectObjectSchemaRegistry`. The SDK client field SHALL be named `SchemaRegistry`.

#### Scenario: List schema registry entries for a project

- **WHEN** a GET request is sent to `/api/schema-registry/projects/:projectId`
- **THEN** the system SHALL return all type entries from `kb.project_object_schema_registry` for the project

#### Scenario: Get single type entry from schema registry

- **WHEN** a GET request is sent to `/api/schema-registry/projects/:projectId/types/:typeName`
- **THEN** the system SHALL return the `ProjectObjectSchemaRegistry` record or HTTP 404 if not found

#### Scenario: Get schema registry stats for a project

- **WHEN** a GET request is sent to `/api/schema-registry/projects/:projectId/stats`
- **THEN** the system SHALL return aggregate statistics (total types, active types, source breakdown) as `SchemaRegistryStats`

#### Scenario: Create new type in schema registry

- **WHEN** a POST request is sent to `/api/schema-registry/projects/:projectId/types` with a valid type definition
- **THEN** the system SHALL insert a new entry into `kb.project_object_schema_registry` and return HTTP 201

#### Scenario: Update type in schema registry

- **WHEN** a PUT request is sent to `/api/schema-registry/projects/:projectId/types/:typeName`
- **THEN** the system SHALL update the entry and return HTTP 200

#### Scenario: Delete type from schema registry

- **WHEN** a DELETE request is sent to `/api/schema-registry/projects/:projectId/types/:typeName`
- **THEN** the system SHALL remove the entry and return HTTP 204

### Requirement: Database table uses kb.project_object_schema_registry naming

The system SHALL store all project type catalog records in `kb.project_object_schema_registry`. The `schema_id` column (renamed from `template_pack_id`) SHALL reference `kb.graph_schemas.id`.

#### Scenario: Type entry references schema by schema_id

- **WHEN** a type entry in the schema registry was populated from an installed MemorySchema
- **THEN** the `schema_id` column SHALL contain the ID of the source `MemorySchema` record in `kb.graph_schemas`

#### Scenario: Custom type has null schema_id

- **WHEN** a type entry has `source = 'custom'` (created manually, not from a schema)
- **THEN** the `schema_id` column SHALL be NULL
