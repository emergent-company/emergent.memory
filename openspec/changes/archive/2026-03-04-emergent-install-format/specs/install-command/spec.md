## ADDED Requirements

### Requirement: Basic Install Command

The CLI SHALL provide an `install` subcommand that reads a config directory and applies its resources to a target Emergent project.

#### Scenario: Install from local folder

- **WHEN** a user runs `emergent install ./my-config`
- **THEN** the CLI SHALL read all files in `packs/` and `agents/` subdirectories
- **AND** create any template packs and agent definitions not already present in the project
- **AND** print a summary line per resource: `created pack "my-research-pack"` or `skipped agent "assistant" (already exists)`
- **AND** exit with code 0 on success

#### Scenario: Install from GitHub URL

- **WHEN** a user runs `emergent install https://github.com/org/repo`
- **THEN** the CLI SHALL fetch the repository contents via the GitHub API or raw archive endpoint
- **AND** apply the same parsing and install logic as a local folder
- **AND** support branch/tag refs via URL fragment: `https://github.com/org/repo#main`

#### Scenario: Install from GitHub URL — private repo

- **WHEN** a user runs `emergent install https://github.com/org/private-repo`
- **AND** `EMERGENT_GITHUB_TOKEN` is set or `--token <token>` is passed
- **THEN** the CLI SHALL include the token in GitHub API requests
- **AND** if no token is provided and the repo is private, the CLI SHALL fail with a clear message: `Repository requires authentication. Set EMERGENT_GITHUB_TOKEN or pass --token.`

#### Scenario: Empty or missing subdirectories

- **WHEN** the install source has no `packs/` directory
- **THEN** the CLI SHALL skip that resource type without error
- **AND** the same applies to `agents/`

### Requirement: Additive-Only Default Behavior

By default, the install command SHALL never overwrite existing resources.

#### Scenario: Pack already exists — skip

- **WHEN** a template pack with the same `name` already exists in the project
- **AND** `--upgrade` is not passed
- **THEN** the CLI SHALL skip the file and print: `skipped pack "my-pack" (already exists, use --upgrade to update)`

#### Scenario: Agent already exists — skip

- **WHEN** an agent definition with the same `name` already exists in the project
- **AND** `--upgrade` is not passed
- **THEN** the CLI SHALL skip the file and print: `skipped agent "my-agent" (already exists, use --upgrade to update)`

### Requirement: Upgrade Flag

The `--upgrade` flag SHALL enable updating existing resources in addition to creating new ones.

#### Scenario: Pack updated with --upgrade

- **WHEN** a user runs `emergent install ./my-config --upgrade`
- **AND** a template pack with the same `name` already exists
- **THEN** the CLI SHALL update the existing pack with the new definition
- **AND** print: `updated pack "my-pack"`

#### Scenario: Agent updated with --upgrade

- **WHEN** a user runs `emergent install ./my-config --upgrade`
- **AND** an agent definition with the same `name` already exists
- **THEN** the CLI SHALL update the existing agent with the new definition
- **AND** print: `updated agent "my-agent"`

#### Scenario: New resources still created with --upgrade

- **WHEN** a user runs `emergent install ./my-config --upgrade`
- **AND** a resource does not yet exist
- **THEN** the CLI SHALL create it as normal
- **AND** print: `created pack "new-pack"`

### Requirement: Dry Run Flag

The `--dry-run` flag SHALL preview all actions without making any API calls or mutations.

#### Scenario: Dry run shows planned actions

- **WHEN** a user runs `emergent install ./my-config --dry-run`
- **THEN** the CLI SHALL print each action it would take, prefixed with `[dry-run]`:
  ```
  [dry-run] would create pack "my-research-pack"
  [dry-run] would skip agent "assistant" (already exists)
  ```
- **AND** no resources SHALL be created, updated, or deleted
- **AND** the CLI SHALL exit with code 0

#### Scenario: Dry run with --upgrade

- **WHEN** a user runs `emergent install ./my-config --dry-run --upgrade`
- **THEN** the CLI SHALL show what would be created AND what would be updated:
  ```
  [dry-run] would create pack "new-pack"
  [dry-run] would update pack "existing-pack"
  [dry-run] would update agent "my-agent"
  ```
- **AND** no mutations SHALL occur

#### Scenario: Dry run with validation errors

- **WHEN** a user runs with `--dry-run` and a file fails schema validation
- **THEN** the CLI SHALL report the validation error as it would in a live run
- **AND** still process remaining valid files to show a complete preview

### Requirement: Install Summary

After all files are processed, the CLI SHALL print a summary of actions taken (or planned, for dry run).

#### Scenario: Live install summary

- **WHEN** an install completes
- **THEN** the CLI SHALL print a summary such as:
  ```
  Install complete: 2 created, 1 updated, 1 skipped, 0 errors
  ```

#### Scenario: Dry run summary

- **WHEN** a dry run completes
- **THEN** the CLI SHALL print a summary such as:
  ```
  Dry run complete: 2 would be created, 1 would be updated, 1 would be skipped
  ```

#### Scenario: Install with errors

- **WHEN** one or more files fail to parse or fail the API call
- **THEN** the CLI SHALL continue processing remaining files
- **AND** include the error count in the summary: `Install complete: 2 created, 0 updated, 0 skipped, 1 error`
- **AND** exit with a non-zero exit code if any errors occurred

### Requirement: Target Project Selection

The install command SHALL apply resources to a specific Emergent project.

#### Scenario: Project specified via flag

- **WHEN** a user passes `--project <project-id>`
- **THEN** the CLI SHALL use that project as the target for all resources

#### Scenario: Project from environment

- **WHEN** `EMERGENT_PROJECT_ID` is set and `--project` is not passed
- **THEN** the CLI SHALL use the environment variable value

#### Scenario: No project specified

- **WHEN** neither `--project` nor `EMERGENT_PROJECT_ID` is set
- **THEN** the CLI SHALL exit with: `Error: no project specified. Pass --project <id> or set EMERGENT_PROJECT_ID.`
