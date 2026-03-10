## ADDED Requirements

### Requirement: Todo Tool Name Constants

The system SHALL define named constants for the todo tool names in `toolpool.go` alongside the existing coordination tool name constants.

#### Scenario: Constants available for injection logic

- **WHEN** `buildTodoTools()` in `executor.go` checks whether an agent definition has opted in
- **THEN** it SHALL use `ToolNameTodoUpdate` and `ToolNameTodoRead` constants (not inline string literals)
- **AND** those constants SHALL be defined in `toolpool.go`

## MODIFIED Requirements

### Requirement: Sub-Agent Tool Restrictions

The existing sub-agent tool restriction requirement is extended: todo tools SHALL NOT be subject to the coordination-tool depth gate.

#### Scenario: Todo tools excluded from coordination-tool depth restriction

- **WHEN** `ResolveTools()` applies the coordination tool depth restriction to a sub-agent
- **THEN** `ToolNameTodoUpdate` and `ToolNameTodoRead` SHALL NOT be removed by that gate
- **AND** todo tools SHALL remain available at any agent depth as long as the agent definition opts in
