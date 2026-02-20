## ADDED Requirements

### Requirement: ADK Session CLI Commands

The CLI SHALL provide a `sessions` (or `adk-sessions`) command group with `list` and `get` subcommands to allow terminal-based inspection of agent reasoning loops.

#### Scenario: Admin lists sessions via CLI

- **WHEN** a user runs `emergent-cli adk-sessions list --project-id 123`
- **THEN** the CLI outputs a formatted table of available ADK sessions, showing ID, creation time, and associated run ID

#### Scenario: Admin inspects a specific session via CLI

- **WHEN** a user runs `emergent-cli adk-sessions get abc-456 --project-id 123`
- **THEN** the CLI outputs the session details and a chronological, human-readable list of LLM events and tool calls
