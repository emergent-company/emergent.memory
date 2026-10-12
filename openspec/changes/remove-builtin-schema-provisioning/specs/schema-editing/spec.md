## MODIFIED Requirements

### Requirement: Show blueprint provenance for object types

Each object type SHALL display the schema it originates from. A type whose origin is an installed
blueprint SHALL be marked as blueprint-derived and identify the owning blueprint by name and version.
A type contributed by the read-time system pack (`is_system=true`, e.g. `session-message-types`)
SHALL render a `System` badge and SHALL NOT be shown as project-authored ("Not from an installed
blueprint").

#### Scenario: Blueprint-derived type

- **WHEN** an object type originates from an installed blueprint
- **THEN** the type is shown with a blueprint-derived marker and the owning blueprint's name and version

#### Scenario: System type

- **WHEN** an object type originates from the read-time system pack (`is_system=true`)
- **THEN** the type is shown with a `System` badge, and SHALL NOT be shown with the project-authored
  "Not from an installed blueprint" marker

#### Scenario: Project-authored type

- **WHEN** an object type was authored directly in the project and does not originate from a blueprint
  or the system pack
- **THEN** the type is shown without a blueprint-derived marker

#### Scenario: Shadowed duplicate is not listed

- **WHEN** two active schema packs declare a type of the same name and the earlier one is shadowed
- **THEN** the Schema area lists only the effective (non-shadowed) type once, and SHALL NOT render a
  duplicate row for the shadowed definition

#### Scenario: Navigate to owning blueprint

- **WHEN** a user follows the owning blueprint reference on a blueprint-derived type
- **THEN** the browser navigates to that blueprint's detail view
