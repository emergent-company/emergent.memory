## MODIFIED Requirements

### Requirement: Show embedding queue progress

The embeddings status page SHALL show, for both objects and relationships, the count of embedding jobs in each state (pending, processing, completed, failed, stale-failed, dead-letter). The `failed` count SHALL include only genuine failures; jobs terminal-failed by the stale-job sweep SHALL be reported separately as stale-failed and SHALL NOT be counted as failed.

#### Scenario: Stats present

- **WHEN** the embeddings status page loads and queue statistics are available
- **THEN** pending, processing, completed, failed, stale-failed, and dead-letter counts are shown for objects and for relationships

#### Scenario: No statistics available

- **WHEN** the embeddings status page loads and no queue statistics are available
- **THEN** a clear "no statistics yet" empty state is shown

#### Scenario: Stale-sweep failures are not presented as current failures

- **WHEN** a queue contains terminal jobs whose error message is the stale-job sweep marker and contains no genuine failures
- **THEN** the failed count is zero
- **AND** the stale-failed count shows the stale-sweep terminal rows

#### Scenario: A queue with only stale failures is not treated as empty

- **WHEN** the embeddings status page loads and every queue count is zero except stale-failed
- **THEN** the queue statistics are shown (the page does not render the empty state)
