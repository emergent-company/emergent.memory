## ADDED Requirements

### Requirement: Derive a blueprint draft from current object types

The API SHALL create a blueprint draft whose manifest object and relationship types are assembled from the project's current compiled types, so an edited project schema can be captured as a reusable blueprint.

#### Scenario: Derive a draft

- **WHEN** a client sends `POST /api/blueprints/derive` with a name and optional version and description
- **THEN** the API returns HTTP 201 with a draft blueprint whose object types and relationship types match the project's current compiled types

#### Scenario: Derived draft does not apply

- **WHEN** a blueprint draft is derived from the current object types
- **THEN** the project schema is unchanged until the draft is explicitly installed

#### Scenario: Duplicate name and version

- **WHEN** a client derives a draft whose name and version already exist
- **THEN** the API returns HTTP 409 with an error and creates no blueprint

#### Scenario: Nothing to derive

- **WHEN** a client derives a blueprint but the project has no compiled types
- **THEN** the API returns HTTP 400 with an error and creates no blueprint

### Requirement: Create a new blueprint version from current object types

The API SHALL create a new draft version of an existing blueprint whose manifest types are replaced with the project's current compiled types, leaving prior versions unchanged.

#### Scenario: New version derived

- **WHEN** a client sends a request to derive a new version of an existing blueprint with a new version label
- **THEN** the API returns a new draft version whose object types match the project's current compiled types

#### Scenario: Prior version unchanged

- **WHEN** a new version is derived from an existing blueprint
- **THEN** the existing published version and its manifest remain unchanged

#### Scenario: Unknown base blueprint

- **WHEN** a client derives a new version for a blueprint id that does not exist
- **THEN** the API returns HTTP 404 with an error and creates no blueprint
