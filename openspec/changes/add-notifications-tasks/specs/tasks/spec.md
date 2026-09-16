## Purpose

Lets a signed-in user monitor Emergent Memory background tasks — operations requiring acknowledgment or action — and resolve or cancel them.

## ADDED Requirements

### Requirement: List tasks

The gateway SHALL list the active project's background tasks and SHALL support filtering by task type and status, with pagination.

#### Scenario: List tasks

- **WHEN** a signed-in user opens the task monitor
- **THEN** the gateway lists the active project's tasks, each with its title, description, type, status, source, and metadata

#### Scenario: Filter by type or status

- **WHEN** a user filters tasks by type or status (pending, resolved, cancelled)
- **THEN** the gateway lists only the matching tasks

#### Scenario: No tasks

- **WHEN** the active project has no tasks
- **THEN** the gateway shows an empty state, not an error

### Requirement: Report task counts

The gateway SHALL report task counts by status so the UI can render a badge for pending tasks.

#### Scenario: Pending-task badge

- **WHEN** a signed-in user loads the console
- **THEN** the gateway returns pending (and resolved) task counts for the active project

### Requirement: Retrieve a task

The gateway SHALL return the full detail of a single task by ID.

#### Scenario: Task detail

- **WHEN** a user opens a task
- **THEN** the gateway returns the task's full detail, including metadata and resolution notes

### Requirement: Resolve a task

The gateway SHALL let a user resolve a pending task, optionally with resolution notes, and SHALL surface the accepted/rejected outcome.

#### Scenario: Resolve with notes

- **WHEN** a user resolves a pending task and provides resolution notes
- **THEN** the gateway forwards the resolve to Memory, the task becomes resolved, and linked notifications are marked read

#### Scenario: Resolve without notes

- **WHEN** a user resolves a pending task without notes
- **THEN** the gateway resolves it with no resolution notes

### Requirement: Cancel a task

The gateway SHALL let a user cancel a pending task that is no longer relevant.

#### Scenario: Cancel pending task

- **WHEN** a user cancels a pending task
- **THEN** the gateway forwards the cancel to Memory and the task becomes cancelled

### Requirement: Reflect background changes

The gateway SHALL refresh the task list when tasks complete in the background, so the monitor reflects pending, resolved, and cancelled tasks without a manual reload.

#### Scenario: Background task completes

- **WHEN** a background task completes or a new task appears
- **THEN** the task monitor refreshes and updates counts

### Requirement: Session- and project-scoped access

The gateway SHALL scope task requests to the signed-in session's active project, using the session's Memory token and project identifier, and MUST NOT expose the token to the browser.

#### Scenario: Task calls use the session token and project

- **WHEN** a signed-in user reads or mutates tasks
- **THEN** the gateway calls Memory with the user's bearer token and the active project identifier

#### Scenario: Token never exposed

- **WHEN** the gateway serves the task list or a task mutation response to the browser
- **THEN** no Memory token appears in the response, HTML, or client-visible headers
