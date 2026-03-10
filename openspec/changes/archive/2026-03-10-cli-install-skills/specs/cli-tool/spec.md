## ADDED Requirements

### Requirement: install-skills Command Group

The CLI SHALL provide an `install-skills` top-level command group for managing Agent Skills in the current project directory.

#### Scenario: Command group is discoverable

- **WHEN** user runs `emergent --help`
- **THEN** `install-skills` appears in the command list with short description `Manage Agent Skills in .agents/skills/`

#### Scenario: Subcommand help

- **WHEN** user runs `emergent install-skills --help`
- **THEN** CLI displays `install`, `list`, `validate`, and `remove` as available subcommands with their short descriptions

#### Scenario: No authentication required

- **WHEN** user runs any `install-skills` subcommand
- **THEN** CLI does NOT require a valid Emergent API token or OAuth session
- **AND** commands work fully offline (except for GitHub/URL sources which require network access to the source host only)
