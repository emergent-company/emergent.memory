## ADDED Requirements

### Requirement: List organizations
The CLI SHALL provide a `memory orgs list` command that lists all organizations the authenticated user is a member of. The command SHALL use account-level authentication. The output SHALL display each organization's name and ID in a numbered list. The command SHALL support JSON output via `--output json` or `--json`.

#### Scenario: List orgs with text output
- **WHEN** user runs `memory orgs list`
- **THEN** the CLI displays a numbered list of organizations with name and ID for each

#### Scenario: List orgs with JSON output
- **WHEN** user runs `memory orgs list --json`
- **THEN** the CLI outputs the org list as a JSON array

#### Scenario: No orgs found
- **WHEN** user runs `memory orgs list` and belongs to no organizations
- **THEN** the CLI displays "No organizations found."

### Requirement: Get organization details
The CLI SHALL provide a `memory orgs get <id>` command that retrieves a single organization by ID. The command SHALL display the organization's name and ID. The command SHALL support JSON output.

#### Scenario: Get org by ID
- **WHEN** user runs `memory orgs get <valid-id>`
- **THEN** the CLI displays the organization's name and ID

#### Scenario: Get org not found
- **WHEN** user runs `memory orgs get <invalid-id>`
- **THEN** the CLI displays an error from the API

### Requirement: Create organization
The CLI SHALL provide a `memory orgs create --name <name>` command that creates a new organization. The `--name` flag SHALL be required. On success, the command SHALL display the new organization's name and ID.

#### Scenario: Create org successfully
- **WHEN** user runs `memory orgs create --name "My Org"`
- **THEN** the CLI creates the organization and displays its name and ID

#### Scenario: Create org missing name
- **WHEN** user runs `memory orgs create` without `--name`
- **THEN** the CLI displays an error that `--name` is required

### Requirement: Delete organization
The CLI SHALL provide a `memory orgs delete <id>` command that deletes an organization by ID. The argument SHALL be the organization UUID. On success, the command SHALL confirm deletion.

#### Scenario: Delete org successfully
- **WHEN** user runs `memory orgs delete <valid-id>`
- **THEN** the CLI deletes the organization and displays a confirmation message

#### Scenario: Delete org not found
- **WHEN** user runs `memory orgs delete <invalid-id>`
- **THEN** the CLI displays an error from the API
