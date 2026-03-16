## ADDED Requirements

### Requirement: Init command registration
The CLI SHALL register a top-level `memory init` command under the root command with no group assignment. The command SHALL accept `--skip-provider` and `--skip-skills` boolean flags.

#### Scenario: Command is available
- **WHEN** a user runs `memory init --help`
- **THEN** the CLI displays usage for the init command showing the `--skip-provider` and `--skip-skills` flags

#### Scenario: Command with no flags
- **WHEN** a user runs `memory init` with no flags
- **THEN** the wizard runs all steps: project selection/creation, provider check, and skills installation

### Requirement: Fresh run — project selection or creation
On a fresh run (no `MEMORY_PROJECT_ID` in `.env.local`), the wizard SHALL fetch the user's projects from the server and present an interactive Bubbletea picker with a synthetic "+ Create new project" item prepended to the list.

#### Scenario: User selects an existing project
- **WHEN** the picker displays existing projects with "+ Create new project" at the top
- **AND** the user selects an existing project
- **THEN** the wizard uses that project's ID and name for subsequent configuration

#### Scenario: User selects "Create new project"
- **WHEN** the user selects "+ Create new project" from the picker
- **THEN** the wizard prompts for a project name using `bufio` with the current folder name as the default shown in brackets
- **AND** pressing Enter with no input accepts the folder name as the project name
- **AND** typing a name overrides the default

#### Scenario: Project creation API call
- **WHEN** the user confirms a project name for creation
- **THEN** the wizard calls the server's project creation API with that name
- **AND** uses the returned project ID and name for subsequent steps

#### Scenario: No existing projects
- **WHEN** the server returns zero projects for the user
- **THEN** the picker still shows the "+ Create new project" option as the only item
- **AND** the user can proceed to create a new project

### Requirement: Token generation and .env.local persistence
After project selection or creation, the wizard SHALL obtain a project API token and write it along with the project context to `.env.local`.

#### Scenario: Existing token available
- **WHEN** a project is selected and it already has API tokens
- **THEN** the wizard retrieves the first available token

#### Scenario: No existing token
- **WHEN** a project is selected and it has no API tokens
- **THEN** the wizard creates a new token named "cli-auto-token" with scopes `data:read`, `data:write`, `schema:read`

#### Scenario: .env.local written
- **WHEN** the token is obtained
- **THEN** the wizard writes `MEMORY_PROJECT_ID`, `MEMORY_PROJECT_NAME`, and `MEMORY_PROJECT_TOKEN` to `.env.local` in the current directory
- **AND** preserves any other existing keys in `.env.local`

### Requirement: Global config update
After writing `.env.local`, the wizard SHALL update the global config file (`~/.memory/config.yaml`) with the selected project ID.

#### Scenario: Global config updated
- **WHEN** `.env.local` is written successfully
- **THEN** the wizard sets `ProjectID` in the global config
- **AND** a failure to update global config prints a warning but does not abort the wizard

### Requirement: .gitignore auto-update
The wizard SHALL ensure `.env.local` is listed in the project's `.gitignore` file without prompting the user.

#### Scenario: .gitignore exists without .env.local entry
- **WHEN** `.gitignore` exists in the current directory but does not contain `.env.local`
- **THEN** the wizard appends `.env.local` to the file

#### Scenario: .gitignore does not exist
- **WHEN** no `.gitignore` file exists in the current directory
- **THEN** the wizard creates `.gitignore` with `.env.local` as its content

#### Scenario: .gitignore already has .env.local
- **WHEN** `.gitignore` already contains `.env.local`
- **THEN** the wizard makes no changes to `.gitignore`

### Requirement: Provider check and configuration
After project setup, the wizard SHALL check if the user's organization has an LLM provider configured. If `--skip-provider` is set, this step is skipped entirely.

#### Scenario: Provider already configured
- **WHEN** the org already has at least one LLM provider config
- **THEN** the wizard prints a confirmation message (e.g., "LLM provider already configured") and skips to the next step

#### Scenario: No provider configured — picker displayed
- **WHEN** no org-level provider config exists
- **THEN** the wizard displays a picker with three options: "Google AI (API key)", "Vertex AI (GCP)", and "Skip for now"

#### Scenario: User selects Google AI
- **WHEN** the user selects "Google AI (API key)" from the provider picker
- **THEN** the wizard prompts for an API key using masked input (`term.ReadPassword`)
- **AND** calls the server's `UpsertOrgConfig` with provider name `google` and the supplied API key
- **AND** runs a provider test call to validate credentials
- **AND** prints success or failure with the test result

#### Scenario: User selects Vertex AI — gcloud present and authenticated
- **WHEN** the user selects "Vertex AI (GCP)"
- **AND** `gcloud` is found via `exec.LookPath`
- **AND** `gcloud auth application-default print-access-token` succeeds
- **THEN** the wizard prompts for GCP project ID and region using `bufio`
- **AND** calls `UpsertOrgConfig` with provider name `google-vertex`, the project ID, and location
- **AND** runs a provider test call to validate credentials

#### Scenario: User selects Vertex AI — gcloud missing or not authenticated
- **WHEN** the user selects "Vertex AI (GCP)"
- **AND** `gcloud` is not found or authentication check fails
- **THEN** the wizard prints step-by-step instructions for installing/authenticating gcloud
- **AND** does not abort the wizard — continues to the next step

#### Scenario: User selects Skip
- **WHEN** the user selects "Skip for now" from the provider picker
- **THEN** the wizard skips provider configuration and proceeds to the next step

#### Scenario: --skip-provider flag
- **WHEN** `memory init --skip-provider` is run
- **THEN** the entire provider step is skipped without any prompts or API calls

### Requirement: Skills installation
After provider setup, the wizard SHALL offer to install Memory skills. If `--skip-skills` is set, this step is skipped entirely.

#### Scenario: User accepts skills installation
- **WHEN** the wizard prompts "Install Memory skills? [Y/n]"
- **AND** the user presses Enter or types "y"/"Y"
- **THEN** the wizard copies all `memory-*` prefixed skills from the embedded catalog to `.agents/skills/` in the current directory
- **AND** existing skills are skipped (not overwritten)

#### Scenario: User declines skills installation
- **WHEN** the user types "n"/"N" at the skills prompt
- **THEN** the wizard skips skills installation

#### Scenario: --skip-skills flag
- **WHEN** `memory init --skip-skills` is run
- **THEN** the entire skills step is skipped without any prompts

### Requirement: Idempotent re-run detection
When `memory init` is run in a directory that already has `MEMORY_PROJECT_ID` in `.env.local`, the wizard SHALL validate the existing configuration and offer to verify or reconfigure each step.

#### Scenario: Valid existing project detected
- **WHEN** `.env.local` contains a `MEMORY_PROJECT_ID`
- **AND** the server confirms the project still exists
- **THEN** the wizard prints "Already initialized for project <name>. Verify settings? [Y/n]"

#### Scenario: User accepts verification
- **WHEN** the user accepts the verify prompt (Enter or "y")
- **THEN** the wizard walks through each step (project, provider, skills) showing the current state and offering to reconfigure

#### Scenario: User declines verification
- **WHEN** the user types "n" at the verify prompt
- **THEN** the wizard exits cleanly with no changes

#### Scenario: Stale project ID
- **WHEN** `.env.local` contains a `MEMORY_PROJECT_ID` that no longer exists on the server
- **THEN** the wizard prints a warning that the project was not found
- **AND** falls through to the fresh-run project selection flow

### Requirement: Non-interactive mode guard
The wizard SHALL detect non-interactive terminals and fail with a clear error message since the wizard requires interactive input.

#### Scenario: Non-interactive terminal
- **WHEN** `memory init` is run in a non-interactive terminal (piped stdin)
- **THEN** the command exits with an error message stating that interactive mode is required

### Requirement: Completion summary
After all steps complete, the wizard SHALL print a summary of what was configured.

#### Scenario: Full run summary
- **WHEN** all wizard steps complete successfully
- **THEN** the wizard prints a summary showing: project name and ID, whether a provider was configured, whether skills were installed, and a suggested next step (e.g., "Run `memory query` to start querying your project")
