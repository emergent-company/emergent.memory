## ADDED Requirements

### Requirement: Registry listing presents the install action inline with the entry

Each registry listing entry SHALL render its call to action in a column to the right of
the entry content, vertically aligned with that content, rather than below it in a
separate footer row. The entry SHALL NOT render a credential-requirement preview;
credential guidance is shown on the configuration form after install.

#### Scenario: Remote entry shows an inline install action

- **WHEN** a listing entry with a remote transport is rendered
- **THEN** its Install action is rendered to the right of the entry content

#### Scenario: Listing entries show no credential preview

- **WHEN** any listing entry is rendered
- **THEN** no credential-requirement preview block is shown on the entry

### Requirement: Registry install records provenance

When memory installs a server from the registry it SHALL persist the registry entry's
name and its repository URL on the created server and SHALL expose them through the
server API. Manual creates SHALL leave provenance unset. The values are advisory
metadata for the configure UI and SHALL NOT affect install or connection behaviour.

#### Scenario: Installed server carries provenance

- **WHEN** a registry entry is installed
- **THEN** the created server exposes the entry's registry name and repository URL

#### Scenario: Manual server has no provenance

- **WHEN** a server is created through the CRUD API rather than a registry install
- **THEN** its registry name and repository URL are unset
