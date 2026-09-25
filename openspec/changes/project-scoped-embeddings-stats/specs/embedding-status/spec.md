## MODIFIED Requirements

### Requirement: Show embedding queue progress

The embeddings status page SHALL show, for both objects and relationships, the count of embedding jobs in each state (pending, processing, completed, failed, stale-failed, dead-letter), scoped to the active project. The page SHALL NOT present counts aggregated across other projects. The `failed` count SHALL include only genuine failures; jobs terminal-failed by the stale-job sweep SHALL be reported separately as stale-failed and SHALL NOT be counted as failed.

#### Scenario: Stats present

- **WHEN** the embeddings status page loads for the active project and that project's queue statistics are available
- **THEN** pending, processing, completed, failed, stale-failed, and dead-letter counts are shown for objects and for relationships, counting only the active project's jobs

#### Scenario: No statistics available

- **WHEN** the embeddings status page loads and the active project has no embedding jobs (even if other projects have jobs)
- **THEN** the "no statistics yet" empty state is shown for the active project
- **AND** other projects' job counts are NOT shown

#### Scenario: Stale-sweep failures are not presented as current failures

- **WHEN** the active project's queue contains terminal jobs whose error message is the stale-job sweep marker and contains no genuine failures
- **THEN** the failed count is zero
- **AND** the stale-failed count shows the stale-sweep terminal rows

#### Scenario: A queue with only stale failures is not treated as empty

- **WHEN** the embeddings status page loads and every active-project queue count is zero except stale-failed
- **THEN** the queue statistics are shown (the page does not render the empty state)

### Requirement: Show embedding worker state

The embeddings status page SHALL show whether the object, relationship, and sweep embedding workers are running or paused, and SHALL show the active worker configuration. Worker state and configuration are instance-wide (the workers are a shared pool) and SHALL NOT be scoped to the active project.

#### Scenario: Worker state present

- **WHEN** the embeddings status page loads
- **THEN** the running/paused state of the object, relationship, and sweep workers is shown

#### Scenario: Configuration present

- **WHEN** the embeddings status page loads
- **THEN** the active worker configuration (batch size, concurrency, polling interval, stale threshold) is shown

## ADDED Requirements

### Requirement: Project-scoped embedding progress endpoint

The server SHALL expose an authenticated, project-scoped embedding progress endpoint that returns object, relationship, and chunk queue counts for one project. The endpoint SHALL require the caller to be a project member; callers not belonging to the project's owning organization SHALL be rejected.

#### Scenario: Member reads own project's progress

- **WHEN** a project member requests embedding progress for their project
- **THEN** object, relationship, and chunk queue counts scoped to that project are returned
- **AND** no other project's jobs are counted

#### Scenario: Non-member is rejected

- **WHEN** a caller who is not a member of the project's owning organization requests that project's embedding progress
- **THEN** the request is rejected with a forbidden error

#### Scenario: Gateway scopes the page to the active project

- **WHEN** the embeddings status page renders with an active project resolved
- **THEN** the gateway requests the project-scoped progress endpoint for that project rather than the instance-wide endpoint

#### Scenario: No active project falls back to instance-wide

- **WHEN** the embeddings status page renders with no project resolvable
- **THEN** the gateway requests the instance-wide progress endpoint
