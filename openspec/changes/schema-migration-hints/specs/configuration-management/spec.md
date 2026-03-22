## ADDED Requirements

### Requirement: Schema assign operation supports migration control flags
The schema assignment request body SHALL accept additional optional fields to control migration behavior when the assigned schema has a `migrations` block.

New fields added to `AssignPackRequest`:
- `force` (bool, default false): bypass the risk gate for dangerous migrations
- `auto_uninstall` (bool, default false): uninstall `from_version` schema after successful migration

#### Scenario: Assign request with force flag
- **WHEN** a user assigns a schema with `force: true`
- **THEN** any auto-triggered migration SHALL proceed regardless of risk level
- **THEN** dropped properties SHALL still be archived before removal

#### Scenario: Assign request with auto_uninstall flag
- **WHEN** a user assigns a schema with `auto_uninstall: true` and migration succeeds
- **THEN** the `from_version` schema SHALL be uninstalled from the project automatically

### Requirement: Schema assign response includes migration result
When auto-migration runs during an assign operation, the `AssignPackResult` response SHALL include a `migration_result` field summarizing what happened.

#### Scenario: Assign response with migration result
- **WHEN** auto-migration runs during assign
- **THEN** the response SHALL include `migration_result` with `objects_migrated`, `objects_failed`, `overall_risk_level`, and `migration_skipped` (false)

#### Scenario: Assign response when migration is skipped
- **WHEN** auto-migration is not triggered (no matching `from_version` installed, or risk gate blocks without force)
- **THEN** the response SHALL include `migration_result.migration_skipped: true` and `migration_result.skip_reason`
