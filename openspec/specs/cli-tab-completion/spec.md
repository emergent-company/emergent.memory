# cli-tab-completion Specification

## Purpose
TBD - created by archiving change enhance-cli-ux. Update Purpose after archive.
## Requirements
### Requirement: Shell completion script generation

The CLI SHALL generate completion scripts for bash, zsh, fish, and PowerShell shells.

#### Scenario: Generate bash completion script

- **WHEN** user runs `emergent-cli completion bash`
- **THEN** system outputs a valid bash completion script to stdout

#### Scenario: Generate zsh completion script

- **WHEN** user runs `emergent-cli completion zsh`
- **THEN** system outputs a valid zsh completion script to stdout

#### Scenario: Show installation instructions

- **WHEN** user runs `emergent-cli completion --help`
- **THEN** system displays installation instructions for each supported shell

### Requirement: Static command and flag completion

The CLI SHALL complete all commands, subcommands, and flags during tab completion.

#### Scenario: Complete subcommand names

- **WHEN** user types `emergent-cli pro<TAB>`
- **THEN** shell completes to `emergent-cli projects`

#### Scenario: Complete flag names

- **WHEN** user types `emergent-cli projects list --out<TAB>`
- **THEN** shell completes to `emergent-cli projects list --output`

#### Scenario: Complete flag values for enum flags

- **WHEN** user types `emergent-cli projects list --output <TAB>`
- **THEN** shell suggests `table`, `json`, `yaml`

### Requirement: Dynamic resource name completion

The CLI SHALL complete resource names (projects, documents) by fetching from the API during tab completion.

#### Scenario: Complete project names

- **WHEN** user types `emergent-cli projects get <TAB>`
- **THEN** shell fetches project list from API and suggests project names

#### Scenario: Complete document IDs within project context

- **WHEN** user types `emergent-cli documents get --project myproj <TAB>`
- **THEN** shell fetches documents for project "myproj" and suggests document IDs

#### Scenario: Handle API timeout gracefully

- **WHEN** API request for completions times out after 2 seconds
- **THEN** shell completes with empty suggestions (no error shown to user)

### Requirement: Completion caching

The CLI SHALL cache resource completions locally to improve performance.

#### Scenario: Cache project names locally

- **WHEN** user completes project names for the first time
- **THEN** system caches results in `~/.emergent/cache/projects.json` with 5-minute TTL

#### Scenario: Use cached completions when fresh

- **WHEN** user completes project names and cache is less than 5 minutes old
- **THEN** system returns cached results without API call

#### Scenario: Refresh stale cache

- **WHEN** user completes project names and cache is older than 5 minutes
- **THEN** system fetches fresh data from API and updates cache

### Requirement: Offline completion fallback

The CLI SHALL provide basic completions when API is unreachable.

#### Scenario: Complete static items when offline

- **WHEN** API is unreachable during tab completion
- **THEN** shell still completes commands and flags (no resource names)

#### Scenario: No error message during completion

- **WHEN** API call fails during tab completion
- **THEN** system silently returns empty suggestions (no error output)

