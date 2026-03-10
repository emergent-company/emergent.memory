## ADDED Requirements

### Requirement: Project context indicator on every command
When a project token is active, the CLI SHALL print a single-line project context indicator to stderr before each command's output.

#### Scenario: Indicator shown with EMERGENT_PROJECT set
- **WHEN** `.env.local` contains `EMERGENT_PROJECT="emergent.memory-dev"`
- **AND** user runs any CLI command (e.g., `memory status`)
- **THEN** stderr emits `Project: emergent.memory-dev  [EMERGENT_PROJECT]` before the command output

#### Scenario: Indicator shown with EMERGENT_PROJECT_TOKEN set
- **WHEN** `.env.local` contains `EMERGENT_PROJECT_TOKEN="emt_abc123"`
- **AND** the project name is available via `EMERGENT_PROJECT_NAME`
- **AND** user runs any CLI command
- **THEN** stderr emits `Project: <project name>  [EMERGENT_PROJECT_TOKEN]` before the command output

#### Scenario: Indicator shows project name when available
- **WHEN** `cfg.ProjectName` is non-empty
- **THEN** the indicator displays the project name
- **AND** does not display the raw token value

#### Scenario: Indicator falls back to token source when name is absent
- **WHEN** `cfg.ProjectName` is empty
- **AND** `cfg.ProjectToken` is non-empty
- **THEN** the indicator displays the masked token and its source label

#### Scenario: Indicator suppressed when no project token is set
- **WHEN** no `EMERGENT_PROJECT`, `EMERGENT_PROJECT_TOKEN`, or `--project-token` flag is set
- **THEN** no indicator is printed to stderr

#### Scenario: Indicator suppressed in non-TTY environments
- **WHEN** stderr is not connected to a terminal (piped or redirected output)
- **THEN** no indicator is printed
- **AND** command output is unaffected

#### Scenario: Indicator suppressed with --no-color / NO_COLOR
- **WHEN** `--no-color` flag is set or `NO_COLOR` env var is present
- **THEN** indicator is still printed but without ANSI color codes

#### Scenario: Source label reflects origin accurately
- **WHEN** project token originates from the `--project-token` CLI flag
- **THEN** source label is `--project-token flag`
- **WHEN** token originates from `EMERGENT_PROJECT_TOKEN` env var (set directly, not promoted)
- **THEN** source label is `EMERGENT_PROJECT_TOKEN`
- **WHEN** token originates from `EMERGENT_PROJECT` env var alias
- **THEN** source label is `EMERGENT_PROJECT`
- **WHEN** token originates from the config file
- **THEN** source label is `config file`
