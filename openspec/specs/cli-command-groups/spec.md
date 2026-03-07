## ADDED Requirements

### Requirement: Commands are organized into labeled groups in help output
The CLI root command SHALL display top-level commands organized into labeled groups using Cobra's native `AddGroup()` / `GroupID` mechanism. The groups SHALL be: **Knowledge Base**, **Agents & AI**, and **Account & Access**.

#### Scenario: User runs memory --help
- **WHEN** a user runs `memory --help` or `memory`
- **THEN** commands are displayed under labeled group headings rather than a flat alphabetical list

#### Scenario: Knowledge Base group contains correct commands
- **WHEN** a user views the help output
- **THEN** the "Knowledge Base" group contains: `documents`, `graph`, `query`, `template-packs`, `blueprints`, `browse`, `embeddings`

#### Scenario: Agents & AI group contains correct commands
- **WHEN** a user views the help output
- **THEN** the "Agents & AI" group contains: `agents`, `agent-definitions`, `adk-sessions`, `mcp-servers`, `mcp-guide`, `provider`, `skills`

#### Scenario: Account & Access group contains correct commands
- **WHEN** a user views the help output
- **THEN** the "Account & Access" group contains: `login`, `logout`, `status`, `set-token`, `tokens`, `config`, `projects`

### Requirement: Developer commands are hidden from default help
The `traces` and `db` commands SHALL have `Hidden: true` so they do not appear in `memory --help` output but remain fully functional when invoked directly.

#### Scenario: traces hidden from help
- **WHEN** a user runs `memory --help`
- **THEN** `traces` does not appear in the output

#### Scenario: traces still works when invoked
- **WHEN** a user runs `memory traces list`
- **THEN** the command executes normally

#### Scenario: db hidden from help
- **WHEN** a user runs `memory --help`
- **THEN** `db` does not appear in the output

#### Scenario: db still works when invoked
- **WHEN** a user runs `memory db diagnose`
- **THEN** the command executes normally

### Requirement: Ungrouped commands appear in Additional Commands section
Commands not assigned a `GroupID` (`completion`, `version`, `server`) SHALL appear in Cobra's default "Additional Commands" section.

#### Scenario: version and completion visible
- **WHEN** a user runs `memory --help`
- **THEN** `version`, `completion`, and `server` appear under "Additional Commands"
