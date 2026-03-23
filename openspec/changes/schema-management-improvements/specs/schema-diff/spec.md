## ADDED Requirements

### Requirement: User can diff an installed schema against an incoming file
The `memory schemas diff <schema-id> --file <path>` command SHALL compare an installed schema (fetched by ID) against an incoming file (JSON or YAML) and print a human-readable change summary. The diff is computed client-side from the stored schema data. The command MUST NOT make any database changes.

#### Scenario: New type in incoming file
- **WHEN** the incoming file contains a type name not present in the installed schema
- **THEN** the diff output shows a `+` line for that type (e.g., `+ objectType "NewFoo" — new type`)

#### Scenario: Removed type in incoming file
- **WHEN** the installed schema contains a type not present in the incoming file
- **THEN** the diff output shows a `-` line for that type with a note about affected objects in the registry

#### Scenario: Renamed relationship type detected
- **WHEN** a relationship type name differs between installed and incoming schemas
- **THEN** it is reported as a removal + addition (no automatic rename detection in v1)

#### Scenario: No changes detected
- **WHEN** installed schema and incoming file define identical type names and properties
- **THEN** the output shows "No changes detected."

#### Scenario: JSON output mode
- **WHEN** user adds `--output json`
- **THEN** output is a JSON object with `added`, `removed`, `modified` arrays of type names

#### Scenario: Missing schema ID returns error
- **WHEN** user provides a schema ID that does not exist or is not accessible
- **THEN** the CLI returns an error: "schema not found"
