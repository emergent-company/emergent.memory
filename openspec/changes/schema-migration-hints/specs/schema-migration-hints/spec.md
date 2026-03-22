## ADDED Requirements

### Requirement: Schema definition supports migration hints
A schema definition file (YAML or JSON) SHALL support an optional `migrations` block at the top level. This block declares the upgrade path from a specific previous version (`from_version`) and includes rename and removal declarations.

The `migrations` block structure:
```yaml
migrations:
  from_version: "1.0.0"
  type_renames:
    - from: OldTypeName
      to: NewTypeName
  property_renames:
    - type_name: NewTypeName
      from: old_prop
      to: new_prop
  removed_properties:
    - type_name: NewTypeName
      name: deprecated_field
```

All sub-fields are optional. The `from_version` field is required if the `migrations` block is present. `removed_properties` entries serve as explicit intent markers: when a listed property is dropped during migration, the system archives it but suppresses the warning (the removal is deliberate, not accidental).

#### Scenario: Schema with migrations block is published
- **WHEN** a user publishes a schema with a valid `migrations` block via `memory schemas push` or `POST /api/schemas`
- **THEN** the server SHALL persist the migrations block in `kb.graph_schemas.migrations` as JSONB
- **THEN** the schema SHALL be retrievable with the migrations block included in the response

#### Scenario: Schema without migrations block is published
- **WHEN** a user publishes a schema without a `migrations` block
- **THEN** the `migrations` field in the DB SHALL be NULL
- **THEN** the schema SHALL behave identically to current behavior — no auto-migration occurs on assign

#### Scenario: Migrations block missing required from_version
- **WHEN** a user publishes a schema with a `migrations` block but no `from_version`
- **THEN** the server SHALL return a 400 error: "migrations.from_version is required when migrations block is present"

### Requirement: Migration hints are validated at publish time
When a schema with a `migrations` block is created or updated, the server SHALL validate that all referenced type names and property names exist in the schema definition.

#### Scenario: Type name in hints does not exist in schema
- **WHEN** a user publishes a schema where `type_renames.from` references a type that does not exist in `object_type_schemas`
- **THEN** the server SHALL return a 400 error listing all invalid references

#### Scenario: Property name in hints does not exist in the type
- **WHEN** a user publishes a schema where `property_renames.from` references a property that does not exist in the referenced type's definition
- **THEN** the server SHALL return a 400 error listing all invalid property references

#### Scenario: Valid hints pass validation
- **WHEN** all type names and property names in the `migrations` block exist in the schema
- **THEN** the schema SHALL be published successfully

### Requirement: Auto-migration triggers asynchronously on schema assign when hints present
When assigning a schema to a project, the server SHALL detect if the new schema has a `migrations` block and if a migration chain can be resolved from the currently installed version. If resolvable, a background migration job SHALL be enqueued and a job ID returned immediately in the assign response.

#### Scenario: Assign schema with resolvable migration chain
- **WHEN** a project has `legal-entities@1.0.0` installed
- **WHEN** the user assigns `legal-entities@1.2.0` which chains through `v1.1.0 → v1.2.0`
- **THEN** the assign SHALL succeed immediately
- **THEN** a migration job SHALL be enqueued for the chain `[v1.0.0→v1.1.0, v1.1.0→v1.2.0]`
- **THEN** the response SHALL include `migration_job_id` and `migration_status: "pending"`

#### Scenario: Assign schema with no matching from_version installed
- **WHEN** a project does NOT have any version of the schema named in `migrations.from_version` chain
- **WHEN** the user assigns the schema
- **THEN** the assign SHALL succeed
- **THEN** no migration job SHALL be enqueued
- **THEN** the response SHALL include `migration_status: "skipped"` with reason "from_version not installed"

#### Scenario: Chain is unresolvable due to missing intermediate version
- **WHEN** `v1.2.0` requires `v1.1.0` as `from_version` but `v1.1.0` is not in the registry at all
- **THEN** the assign SHALL succeed
- **THEN** no migration job SHALL be enqueued
- **THEN** the response SHALL include `migration_status: "chain_unresolvable"` and name the missing version

#### Scenario: Auto-migration blocked due to dangerous risk level
- **WHEN** auto-migration preview (run synchronously before enqueue) detects dangerous risk
- **WHEN** the assign request does NOT include `force: true`
- **THEN** the assignment SHALL still be created
- **THEN** no migration job SHALL be enqueued
- **THEN** the response SHALL include `migration_status: "blocked"` with `block_reason`

#### Scenario: Auto-migration with force flag enqueues regardless of risk
- **WHEN** the assign request includes `force: true`
- **THEN** the migration job SHALL be enqueued regardless of risk level

#### Scenario: Dry-run assign returns migration preview
- **WHEN** the assign request has `dry_run: true` and the schema has migration hints
- **THEN** the response SHALL include `migration_preview` with the full risk assessment
- **THEN** no assignment row SHALL be created and no migration job SHALL be enqueued

#### Scenario: Auto-uninstall previous schema version after job completes
- **WHEN** the migration job completes successfully
- **WHEN** the assign request included `auto_uninstall: true`
- **THEN** the `from_version` schema SHALL be uninstalled from the project
- **THEN** its type registry entries SHALL be removed

### Requirement: Multi-hop migration chain resolution
When assigning a schema version, the server SHALL resolve a migration chain by walking backwards through `from_version` declarations in the registry, starting from the installed version.

#### Scenario: Chain resolves across multiple hops
- **WHEN** `v1.0.0` is installed and `v1.3.0` is being assigned
- **WHEN** the chain `v1.0.0→v1.1.0→v1.2.0→v1.3.0` can be resolved from the registry
- **THEN** all three hops SHALL execute sequentially in the background job
- **THEN** each hop's rename mappings SHALL be applied before the next hop starts

#### Scenario: Chain capped at 10 hops
- **WHEN** chain resolution would require more than 10 hops
- **THEN** the server SHALL return `migration_status: "chain_unresolvable"` with reason "chain exceeds 10 hop limit"

#### Scenario: Intermediate versions do not need to be installed
- **WHEN** `v1.1.0` is not installed on the project but is present in the schema registry
- **THEN** the chain SHALL still resolve and execute through `v1.1.0` as an intermediate step
