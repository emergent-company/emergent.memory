## ADDED Requirements

### Requirement: Database Migrations Management
The agent SHALL be able to manage database migrations using Taskfile commands (`task migrate:up`, `task migrate:down`, `task migrate:status`).

#### Scenario: Applying Migrations
- **WHEN** the user asks to apply pending database migrations
- **THEN** the agent runs `task migrate:up`

#### Scenario: Checking Migration Status
- **WHEN** the user asks to check the migration status
- **THEN** the agent runs `task migrate:status` and provides the current state
