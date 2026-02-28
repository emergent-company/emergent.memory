## ADDED Requirements

### Requirement: Automatic Project Context Loading
The `emergent` CLI SHALL automatically search for and load `EMERGENT_PROJECT_TOKEN`, `EMERGENT_PROJECT_ID`, and `EMERGENT_PROJECT_NAME` from `.env.local` or `.env` files in the current directory if no token/project flag is provided.

#### Scenario: Context found in .env.local
- **WHEN** the CLI executes a project-scoped command without `--project-id`
- **AND** `.env.local` contains `EMERGENT_PROJECT_ID=local_id` and `EMERGENT_PROJECT_NAME=local_name`
- **THEN** the CLI SHALL use `local_id` for project operations
- **AND** it SHALL print a message indicating the project context being used (e.g., `Using project context: local_name (from .env.local)`)

#### Scenario: Context found in .env
- **WHEN** the CLI executes a project-scoped command without `--project-id`
- **AND** `.env.local` does not exist or does not contain the context
- **AND** `.env` contains `EMERGENT_PROJECT_ID=env_id`
- **THEN** the CLI SHALL use `env_id` for project operations
- **AND** it SHALL print a message indicating the project context was loaded from `.env`

#### Scenario: Flag overrides environment file
- **WHEN** the CLI is executed with `--project-id=flag_id`
- **AND** `.env` contains `EMERGENT_PROJECT_ID=env_id`
- **THEN** the CLI SHALL use `flag_id` for project operations

#### Scenario: No context found
- **WHEN** the CLI executes a project-scoped command without `--project-id`
- **AND** no `EMERGENT_PROJECT_ID` is found in environment or `.env` files
- **THEN** commands requiring a project context SHALL fail with an error message
