## ADDED Requirements

### Requirement: Project context file reflects current backend stack
The `openspec/project.md` file SHALL accurately describe the current technology stack, including all runtime changes (e.g., backend framework migrations), so that AI agents receive correct context when creating artifacts.

#### Scenario: Agent reads project context for backend work
- **WHEN** an AI agent reads `openspec/project.md` to understand the backend architecture
- **THEN** the file SHALL describe Go (Echo, Bun ORM, fx, ADK-Go) as the backend, not NestJS or TypeORM
- **AND** all app paths SHALL reference `apps/server-go`, not `apps/server`
- **AND** migration tooling SHALL be listed as Goose, not TypeORM migrations

#### Scenario: Agent reads project context for testing
- **WHEN** an AI agent reads `openspec/project.md` to understand the testing strategy
- **THEN** the file SHALL describe Go testify suites for backend tests
- **AND** test commands SHALL reference `server-go` targets (e.g., `nx run server-go:test-e2e`)

#### Scenario: Agent reads project context for git workflow
- **WHEN** an AI agent reads `openspec/project.md` to understand the git workflow
- **THEN** the main branch SHALL be listed as `main`, not `master`
