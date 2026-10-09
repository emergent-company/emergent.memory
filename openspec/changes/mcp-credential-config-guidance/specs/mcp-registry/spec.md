## ADDED Requirements

### Requirement: Registry browse exposes each remote's declared credentials

The registry browse response SHALL include, for each remote, the headers and
environment variables that remote declares (with the registry's `name`, `isRequired`,
`isSecret`, `description`, and header `value` template). The catalog SHALL preview the
credentials that an install will require, using the same remote-selection order the
install uses (streamable-http before sse), so a user can prepare them before installing.

#### Scenario: Installable remote's required header is previewed

- **WHEN** a catalog entry whose selected remote declares an `Authorization` secret header is rendered
- **THEN** the card names `Authorization` and shows the registry's description for it

#### Scenario: A remote requiring nothing shows no preview

- **WHEN** a catalog entry's selected remote declares no required-or-secret header or env var
- **THEN** the card shows no credential preview block

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
