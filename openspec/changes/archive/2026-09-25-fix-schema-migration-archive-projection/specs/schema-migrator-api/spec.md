## ADDED Requirements

### Requirement: Batch migration operations load the migration archive column
The system SHALL load `kb.graph_objects.migration_archive` whenever it lists objects for a schema migrate or rollback operation. The archive column SHALL remain excluded from the default graph list/search projection so archive JSON is not exposed by those endpoints.

#### Scenario: Rollback lists objects with their archive
- **WHEN** `RollbackSchemaMigration` lists project objects
- **THEN** each returned object SHALL carry its persisted `migration_archive` entries
- **THEN** objects with at least one archive entry for the requested `to_version` SHALL NOT be skipped for an empty archive slice

#### Scenario: Forward migration lists objects with their archive
- **WHEN** `ExecuteSchemaMigration` lists objects of an affected type
- **THEN** each returned object SHALL carry its persisted `migration_archive` entries
- **THEN** `SchemaMigrator.MigrateObject` SHALL append the new archive entry to the existing entries

#### Scenario: Public list/search projection is unchanged
- **WHEN** a caller lists or searches graph objects without requesting the archive
- **THEN** the `migration_archive` column SHALL NOT be selected
- **THEN** the response SHALL NOT include archive JSON

### Requirement: Rollback restores archived property data and consumes the entry
The rollback operation SHALL restore the `dropped_data` of the matching archive entry onto the object's properties, update `schema_version` to the archive entry's `from_version`, and remove the consumed entry from `migration_archive`.

#### Scenario: Rollback restores the dropped property
- **GIVEN** an object whose property was archived by a migration to version `X`
- **WHEN** rollback is requested with `to_version: "X"`
- **THEN** the archived property SHALL be restored to the object's `properties`
- **THEN** `schema_version` SHALL be set to the archive entry's `from_version`
- **THEN** the response `objects_restored` SHALL count the object

#### Scenario: Rollback consumes the archive entry
- **GIVEN** an object with a single archive entry for version `X`
- **WHEN** rollback is requested with `to_version: "X"`
- **THEN** that archive entry SHALL be removed from `migration_archive`
- **THEN** a subsequent rollback to the same version SHALL be a no-op for that object

### Requirement: Multi-hop migrations append to the archive without clobbering earlier entries
Each successive migration hop SHALL append its archive entry to any entries already present on the object. Earlier hops' entries SHALL remain intact and independently rollback-able.

#### Scenario: Second hop preserves the first hop's archive entry
- **GIVEN** an object migrated from `1.0.0` to `2.0.0`, archiving property `alpha`
- **WHEN** the object is migrated from `2.0.0` to `3.0.0`, archiving property `beta`
- **THEN** `migration_archive` SHALL contain both the `to_version: "2.0.0"` and `to_version: "3.0.0"` entries
- **THEN** rolling back to `3.0.0` SHALL restore `beta` and leave the `2.0.0` entry in place
- **THEN** rolling back to `2.0.0` SHALL restore `alpha`

### Requirement: Batch migration operations visit every matching object
The migrate and rollback operations SHALL visit every matching object in the project, not only the first page. When a project holds more objects than a single list page, the operations SHALL page through the full result set.

#### Scenario: Migration covers objects beyond the first page
- **GIVEN** a project with more objects of an affected type than the configured maximum list page size
- **WHEN** a migration executes
- **THEN** every affected object SHALL be migrated and counted in `objects_migrated`

#### Scenario: Rollback covers objects beyond the first page
- **GIVEN** a project with more archived objects than the configured maximum list page size
- **WHEN** a rollback executes
- **THEN** every archived object SHALL be restored and counted in `objects_restored`
