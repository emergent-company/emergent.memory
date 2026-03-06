## ADDED Requirements

### Requirement: PersistentPreRunE project context hook
The root Cobra command SHALL have a `PersistentPreRunE` hook that fires before every subcommand and prints the project context indicator when a project token is active.

#### Scenario: Hook fires before every subcommand
- **WHEN** any subcommand is executed
- **THEN** `PersistentPreRunE` on the root command runs first
- **AND** the project context indicator is printed to stderr if a token is active

#### Scenario: Hook does not interfere with command output
- **WHEN** `PersistentPreRunE` runs
- **THEN** all indicator output goes exclusively to stderr
- **AND** stdout receives only the command's own output

#### Scenario: Hook skips indicator for commands with no project token
- **WHEN** `cfg.ProjectToken` is empty after config load
- **THEN** `PersistentPreRunE` completes without printing anything

## MODIFIED Requirements

### Requirement: Configuration Management
The CLI SHALL manage server configuration and user preferences via config commands.

#### Scenario: Set server URL
- **WHEN** user runs `emergent-cli config set-server --url https://api.example.com`
- **THEN** server URL is saved to `~/.emergent/config.yaml`
- **AND** CLI confirms "Server URL updated"

#### Scenario: Show current configuration
- **WHEN** user runs `emergent-cli config show`
- **THEN** CLI displays current configuration including server URL, default org/project, and output format
- **AND** masks sensitive values (passwords, tokens)

#### Scenario: Set default organization and project
- **WHEN** user runs `emergent-cli config set-defaults --org org_123 --project proj_456`
- **THEN** defaults are saved to config file
- **AND** subsequent commands use these defaults when `--org` and `--project` flags are omitted

#### Scenario: Configuration precedence
- **WHEN** user provides `--server` flag on command line
- **AND** config file has different server URL
- **THEN** command-line flag takes precedence over config file

#### Scenario: EMERGENT_PROJECT alias accepted in env files
- **WHEN** `.env.local` or `.env` contains `EMERGENT_PROJECT=<value>`
- **AND** `EMERGENT_PROJECT_TOKEN` is not set
- **THEN** CLI treats `EMERGENT_PROJECT` as equivalent to `EMERGENT_PROJECT_TOKEN`
- **AND** uses the value to scope all commands to that project
