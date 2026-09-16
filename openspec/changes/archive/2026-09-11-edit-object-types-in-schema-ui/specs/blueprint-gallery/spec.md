## ADDED Requirements

### Requirement: Create a blueprint from the current schema

The UI SHALL let a user create a blueprint draft from the project's current object types, either as a new blueprint or as a new version of an existing blueprint, and show the resulting draft in the drafts list.

#### Scenario: Save current schema as a new blueprint

- **WHEN** a user invokes "Save as blueprint" from the Schema area or blueprints gallery, provides a name, and confirms
- **THEN** a draft blueprint is created from the current object types and appears in the blueprints drafts list

#### Scenario: Save current schema as a new version

- **WHEN** a user invokes "Save as new version" on an existing blueprint and provides a version label
- **THEN** a new draft version is created from the current object types and appears in that blueprint's version list

#### Scenario: Draft is installable

- **WHEN** a user installs a blueprint draft derived from the current schema
- **THEN** the install succeeds through the existing install path and reports the applied types

#### Scenario: Derivation fails validation

- **WHEN** the derivation request is rejected (for example, duplicate name and version)
- **THEN** the UI shows the error and does not add a draft to the list

### Requirement: Show blueprint provenance in the gallery

The gallery and blueprint detail views SHALL identify blueprint-derived object types and link them to the owning blueprint.

#### Scenario: Detail lists derived types

- **WHEN** a user opens a blueprint's detail view
- **THEN** its object types are listed with the owning blueprint identified

#### Scenario: Navigate from type to blueprint

- **WHEN** a user follows the owning-blueprint reference shown for a derived type
- **THEN** the browser navigates to that blueprint's detail view
