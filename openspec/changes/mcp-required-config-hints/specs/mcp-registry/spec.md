## ADDED Requirements

### Requirement: Required configuration keys carry the registry's guidance

When memory resolves a registry entry's required configuration, it SHALL retain the
registry's own description for each header and environment-variable key, and for header
keys SHALL also retain the registry-declared value template (for example
`Bearer {API_TOKEN}`). Memory SHALL persist this guidance on the created server and
return it in the API's required-configuration representation, so a client can show what
each key expects rather than only its name. Each field is optional: entries that omit
them SHALL yield empty values, never an error.

#### Scenario: GitHub-shaped header description is retained

- **WHEN** a registry entry's selected remote declares an `Authorization` header with `isSecret: true` and a description
- **THEN** the persisted and returned required key for `Authorization` includes that description

#### Scenario: Environment variable description is retained

- **WHEN** a registry entry's selected remote declares a required environment variable with a description
- **THEN** the persisted and returned required key for that variable includes the description

#### Scenario: Header value template is retained as a hint

- **WHEN** a registry entry's selected remote declares a required header with a `value` template such as `Bearer {API_TOKEN}`
- **THEN** the required key records that template as its value hint

#### Scenario: Missing guidance is not an error

- **WHEN** a registry entry declares a required key with no description and no value template
- **THEN** the required key is persisted with empty guidance fields and resolution succeeds
