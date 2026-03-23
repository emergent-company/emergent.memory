## ADDED Requirements

### Requirement: Unified schema list endpoint returns all schemas in one call
The system SHALL provide `GET /api/schemas/projects/:id/all` that returns all schemas visible to the project — both installed and available — as a single list. Each item SHALL include an `installed` boolean, and when installed, also `installedAt`, `assignmentId`, and `blueprintSource`.

#### Scenario: All schemas returned together
- **WHEN** `GET /api/schemas/projects/:id/all` is called
- **THEN** the response SHALL include schemas that are installed AND schemas that are available but not yet installed
- **AND** installed schemas SHALL have `"installed": true`
- **AND** available schemas SHALL have `"installed": false`

#### Scenario: No duplicates in unified list
- **WHEN** a schema is installed in the project
- **AND** `GET /api/schemas/projects/:id/all` is called
- **THEN** that schema SHALL appear exactly once in the response with `"installed": true`

### Requirement: CLI schemas list shows unified view by default
The `memory schemas list` command SHALL by default display all schemas (installed + available) in a table with STATUS and SOURCE columns. The `--installed` flag SHALL filter to installed-only. The `--available` flag SHALL filter to available-only.

#### Scenario: Default list shows all schemas with status column
- **WHEN** `memory schemas list` is run without flags
- **THEN** the output SHALL show a table with at minimum NAME, VERSION, STATUS, and SOURCE columns
- **AND** installed schemas SHALL show STATUS = `installed`
- **AND** available schemas SHALL show STATUS = `available`

#### Scenario: Blueprint-sourced schemas show source in list
- **WHEN** `memory schemas list` is run
- **AND** a schema was installed from a blueprint with source `/path/to/schema.yaml`
- **THEN** the SOURCE column for that schema SHALL display the blueprint source path

#### Scenario: --installed flag narrows to installed only
- **WHEN** `memory schemas list --installed` is run
- **THEN** only schemas with STATUS = `installed` SHALL be shown

#### Scenario: --available flag narrows to available only
- **WHEN** `memory schemas list --available` is run
- **THEN** only schemas with STATUS = `available` SHALL be shown
