## MODIFIED Requirements

### Requirement: Show blueprint provenance for object types

Each object type SHALL display the schema it originates from. A type whose origin is an installed
blueprint SHALL be marked as blueprint-derived and identify the owning blueprint by name and version.
A type with no owning blueprint SHALL be shown as project-authored ("Not from an installed
blueprint"). There is no read-time system pack.

#### Scenario: Blueprint-derived type

- **WHEN** an object type originates from an installed blueprint
- **THEN** the type is shown with a blueprint-derived marker and the owning blueprint's name and version

#### Scenario: System type

- **WHEN** object types are loaded
- **THEN** no type SHALL be shown with a `System` badge — the read-time system pack (`is_system=true`) is
  removed, so every type is either blueprint-derived or project-authored

#### Scenario: Project-authored type

- **WHEN** an object type was authored directly in the project and does not originate from a blueprint
- **THEN** the type is shown without a blueprint-derived marker

#### Scenario: Shadowed duplicate is not listed

- **WHEN** two active schema packs declare a type of the same name and the earlier one is shadowed
- **THEN** the Schema area lists only the effective (non-shadowed) type once, and SHALL NOT render a
  duplicate row for the shadowed definition

#### Scenario: Navigate to owning blueprint

- **WHEN** a user follows the owning blueprint reference on a blueprint-derived type
- **THEN** the browser navigates to that blueprint's detail view
