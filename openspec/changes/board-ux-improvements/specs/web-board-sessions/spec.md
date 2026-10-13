## ADDED Requirements

### Requirement: The chat area exposes board runs as sessions

The chat session rail SHALL offer a board origin filter and SHALL list a
project's board runs as rows that open the run's transcript in the chat
workspace, so an operator can see what the agent did for a task.

#### Scenario: Filtering by board origin

- **WHEN** the operator selects the board origin in the session rail
- **THEN** only board-origin rows remain visible

#### Scenario: Opening a board run

- **WHEN** the operator opens a board-origin row
- **THEN** the run's transcript is rendered in the chat workspace

#### Scenario: Rows carry task context

- **WHEN** a board run's subject resolves to a work item in the project
- **THEN** the row presents that work item's context rather than only the run id

#### Scenario: Unresolvable subject degrades

- **WHEN** a board run's subject does not resolve to a work item
- **THEN** the row still renders and falls back to run-derived context instead of being dropped

#### Scenario: A failure to list board runs does not break the rail

- **WHEN** the board-runs listing fails
- **THEN** the session rail still renders its other origins

#### Scenario: The board reaches its own sessions

- **WHEN** the operator is looking at the board
- **THEN** a board run's transcript can be opened from the board surface without first locating the session in the chat rail
