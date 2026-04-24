## ADDED Requirements

### Requirement: Sessions CLI subcommand
The CLI SHALL expose a `memory sessions` subcommand with: `create --title <t> [--summary <s>] [--agent-version <v>]`, `list [--limit N]`, `get <id>`.

#### Scenario: Create session via CLI
- **WHEN** `memory sessions create --title "chat-001"` is run
- **THEN** a session MUST be created and its ID printed to stdout

### Requirement: Sessions messages CLI subcommand
The CLI SHALL expose `memory sessions messages` with: `add <session-id> --role <user|assistant|system> --content <text>`, `list <session-id> [--limit N]`.

#### Scenario: Add message via CLI
- **WHEN** `memory sessions messages add <id> --role user --content "hello"` is run
- **THEN** the message MUST be appended and its sequence_number printed
