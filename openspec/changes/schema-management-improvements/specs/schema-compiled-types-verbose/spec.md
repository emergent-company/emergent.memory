## ADDED Requirements

### Requirement: Compiled-types output shows schema version and shadow warnings
The `memory schemas compiled-types` command SHALL accept a `--verbose` flag. When provided, each type in the output SHALL include:
- `schemaVersion`: the version string of the schema that contributed this type
- `schemaId`: the ID of the contributing schema
- `shadowed`: `true` if another schema installed earlier defines a type with the same name (the later schema wins)

Without `--verbose` the output is unchanged from the current behavior.

#### Scenario: Verbose shows schema version per type
- **WHEN** user runs `memory schemas compiled-types --verbose`
- **THEN** each object type and relationship type entry includes `schemaId` and `schemaVersion` fields
- **AND** the output is valid JSON (or pretty-printed table if `--output table` is used)

#### Scenario: Shadow warning for duplicate type names
- **WHEN** two installed schemas both define an object type with the same name
- **THEN** the type from the later-installed schema appears in the output with `"shadowed": false`
- **AND** the type from the earlier-installed schema appears with `"shadowed": true`
- **AND** a warning line is printed to stderr: `WARNING: type "Foo" is shadowed by schema <id>`

#### Scenario: No shadows — clean output
- **WHEN** no type name collisions exist across installed schemas
- **THEN** no warnings are printed and no entry has `"shadowed": true`

#### Scenario: Non-verbose behavior is unchanged
- **WHEN** user runs `memory schemas compiled-types` (no `--verbose`)
- **THEN** the output is identical to the current behavior (no schema version or shadow fields)
