## ADDED Requirements

### Requirement: Init detects missing organization
The `memory init` command SHALL check whether the authenticated user has any organizations before entering the project selection flow. This check SHALL happen immediately after client creation.

#### Scenario: User has organizations
- **WHEN** user runs `memory init` and belongs to at least one organization
- **THEN** init proceeds directly to project selection/creation as before

#### Scenario: User has no organizations (fresh account)
- **WHEN** user runs `memory init` and belongs to zero organizations
- **THEN** init displays a message explaining that an organization is needed and prompts the user to create one

### Requirement: Init offers interactive org creation
When the user has no organizations, `memory init` SHALL offer to create one interactively. The user SHALL be prompted for an organization name, with the current directory name as the default. After successful creation, init SHALL proceed to the project selection flow using the new org.

#### Scenario: User accepts org creation
- **WHEN** init detects no orgs and user agrees to create one
- **AND** user enters an org name (or accepts the default)
- **THEN** init creates the organization via the API and proceeds to project setup

#### Scenario: User declines org creation
- **WHEN** init detects no orgs and user declines to create one
- **THEN** init exits with a message explaining how to create an org manually (`memory orgs create --name <name>`)

### Requirement: Init org check does not affect re-runs
The org check SHALL only apply during the fresh-run path of `memory init`. During re-runs (when `.env.local` already contains `MEMORY_PROJECT_ID`), the org check SHALL be skipped since the project already exists and its org is established.

#### Scenario: Re-run with existing project
- **WHEN** user runs `memory init` and `.env.local` has a valid `MEMORY_PROJECT_ID`
- **THEN** init follows the re-run verification flow without checking org existence
