## ADDED Requirements

### Requirement: CLI reference page documents memory init
The developer guide SHALL include documentation for `memory init` covering its purpose (interactive wizard to set up a Memory project in a directory), the env vars it writes (`MEMORY_PROJECT_ID`, `MEMORY_PROJECT_NAME`, `MEMORY_PROJECT_TOKEN`), and its flags (`--skip-provider`, `--skip-skills`).

#### Scenario: Developer finds init documentation
- **WHEN** a developer navigates to the developer guide CLI reference page
- **THEN** they SHALL find a section for `memory init` that describes the wizard steps (project selection/creation, provider config, skills installation) and the `.env.local` output

#### Scenario: Flags are documented
- **WHEN** the init section is rendered
- **THEN** `--skip-provider` and `--skip-skills` flags SHALL be listed with their effect

### Requirement: CLI reference page documents memory ask
The developer guide SHALL include documentation for `memory ask` covering its context-aware behavior (unauthenticated, auth-no-project, auth-with-project), example invocations, and the `--project` flag.

#### Scenario: Developer finds ask documentation
- **WHEN** a developer navigates to the CLI reference page
- **THEN** they SHALL find a section for `memory ask` with at least three usage examples

#### Scenario: Context modes are explained
- **WHEN** the ask section is rendered
- **THEN** the three context modes (no auth, auth without project, auth with project) SHALL be described

### Requirement: CLI reference page documents memory adk-sessions
The developer guide SHALL include documentation for `memory adk-sessions` (alias: `sessions`) covering the `list` and `get` subcommands and their purpose (inspecting ADK session event history).

#### Scenario: adk-sessions subcommands are shown
- **WHEN** the adk-sessions section is rendered
- **THEN** both `list` and `get` subcommands SHALL appear with example invocations

### Requirement: CLI reference page documents memory mcp-guide
The developer guide SHALL include documentation for `memory mcp-guide` as a quick way to generate MCP client configuration snippets (Claude Desktop, Cursor) using the active server URL and API key.

#### Scenario: mcp-guide is cross-referenced from the MCP servers page
- **WHEN** a developer reads the MCP servers developer guide page
- **THEN** there SHALL be a note or link pointing to `memory mcp-guide` for generating client config snippets

#### Scenario: mcp-guide entry appears in CLI reference
- **WHEN** the CLI reference page is rendered
- **THEN** `memory mcp-guide` SHALL have its own section with a usage example

### Requirement: CLI reference page documents memory install-memory-skills
The developer guide SHALL include documentation for `memory install-memory-skills` covering its purpose (install built-in `memory-*` skills into `.agents/skills/`), the `--dir` and `--force` flags, and when to use it.

#### Scenario: install-memory-skills is documented
- **WHEN** the CLI reference page is rendered
- **THEN** `memory install-memory-skills` SHALL appear with a description and at least one example command
