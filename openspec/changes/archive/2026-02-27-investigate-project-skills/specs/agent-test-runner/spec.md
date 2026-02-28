## ADDED Requirements

### Requirement: Test Execution
The agent SHALL be able to run project tests using predefined Taskfile commands (`task test`, `task test:e2e`, `task test:integration`).

#### Scenario: Running Unit Tests
- **WHEN** the user asks to run backend unit tests
- **THEN** the agent runs `task test` and reports the results

#### Scenario: Running E2E Tests
- **WHEN** the user asks to run backend e2e tests
- **THEN** the agent runs `task test:e2e` and reports the results
