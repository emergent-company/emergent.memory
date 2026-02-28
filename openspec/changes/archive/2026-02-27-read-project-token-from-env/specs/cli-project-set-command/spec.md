## ADDED Requirements

### Requirement: Interactive Project Selection
The CLI SHALL provide an `emergent projects set` command that allows users to interactively select a project if no argument is provided.

#### Scenario: User runs command without arguments
- **WHEN** the user executes `emergent projects set`
- **AND** they are authenticated
- **THEN** the CLI SHALL display a list of available projects
- **AND** prompt the user to select one interactively

### Requirement: Direct Project Selection
The CLI SHALL allow users to specify a project name or ID directly via an argument.

#### Scenario: User provides project ID
- **WHEN** the user executes `emergent projects set <project-id>`
- **AND** the project exists and they have access
- **THEN** the CLI SHALL skip the interactive prompt and select the specified project

### Requirement: Token Generation and Storage
Upon project selection, the CLI SHALL retrieve or generate a token and save it to `.env.local` along with the project ID and name.

#### Scenario: Successful token generation and storage
- **WHEN** a project is selected (interactively or via argument)
- **THEN** the CLI SHALL retrieve an existing valid token or generate a new one via the API
- **AND** it SHALL write `EMERGENT_PROJECT_TOKEN=<token>`, `EMERGENT_PROJECT_ID=<id>`, and `EMERGENT_PROJECT_NAME=<name>` to the `.env.local` file in the current directory
- **AND** if `.env.local` exists, it SHALL update or append the variables without removing other contents
- **AND** it SHALL print a success message indicating the project context is now set locally
