## Purpose

Surfaces embedding-generation status and progress so users can see whether knowledge-graph objects and relationships have been embedded and whether the embedding workers are healthy. Queue counters alone are not enough: terminal job rows are purged on a retention window, so an idle-but-fully-embedded project must still be able to show that its vectors exist.

## ADDED Requirements

### Requirement: Show embedding coverage

The embeddings status page SHALL show, for the active project and for both objects and relationships, how many graph rows already hold an embedding vector (`embedded`), how many are still awaiting one (`awaiting`), and the live total (`total`), so an idle project that is fully embedded is distinguishable from one that has never processed anything.

#### Scenario: Coverage present

- **WHEN** the embeddings status page loads and coverage is available
- **THEN** embedded, awaiting, and total counts are shown for objects and for relationships

#### Scenario: Everything embedded

- **WHEN** the project's live objects and relationships all hold vectors and no queue work is pending
- **THEN** the page states that there is no pending embedding work and shows the embedded totals
- **AND** the page does not present a bare "no statistics yet" state

#### Scenario: Work awaiting

- **WHEN** some live objects or relationships have no vector
- **THEN** the awaiting count is shown for the affected queue

#### Scenario: Coverage fetch fails

- **WHEN** the coverage fetch fails
- **THEN** the coverage section shows an error state
- **AND** the queue and worker sections remain usable

### Requirement: Serve embedding coverage from a project-scoped endpoint

The backend SHALL expose `GET /api/embeddings/coverage` returning, per queue, the `embedded`, `awaiting`, and `total` counts. Coverage SHALL count live (non-deleted) graph rows; `total` SHALL equal `embedded + awaiting`. The endpoint SHALL use the same authorization posture as `GET /api/embeddings/progress`: project-scoped for a caller with project context, and deployment-wide only for an active `superadmin_full` grant.

#### Scenario: Project member reads coverage

- **WHEN** a project member requests coverage with their project context
- **THEN** the counts are scoped to that project

#### Scenario: Coverage without project context

- **WHEN** an authenticated caller without an active `superadmin_full` grant requests coverage without a project context
- **THEN** the server responds 403

#### Scenario: Counts do not scan the graph tables

- **WHEN** coverage is requested for a project with hundreds of thousands of live rows
- **THEN** the embedded and awaiting counts are served by partial indexes over the embedding predicates, without a sequential scan of `kb.graph_objects` or `kb.graph_relationships`

## MODIFIED Requirements

### Requirement: Show embedding queue progress

The embeddings status page SHALL show, for both objects and relationships, the count of embedding jobs in each state (pending, processing, completed, failed, stale-failed, dead-letter). The `failed` count SHALL include only genuine failures; jobs terminal-failed by the stale-job sweep SHALL be reported separately as stale-failed and SHALL NOT be counted as failed. The empty state SHALL distinguish "no data" from "idle and fully embedded": when coverage is available, an all-zero queue SHALL be presented as no pending embedding work rather than as absent statistics.

#### Scenario: Stats present

- **WHEN** the embeddings status page loads and queue statistics are available
- **THEN** pending, processing, completed, failed, stale-failed, and dead-letter counts are shown for objects and for relationships

#### Scenario: No statistics available

- **WHEN** the embeddings status page loads and every queue count is zero
- **AND** coverage is available and reports zero awaiting work
- **THEN** the page presents the coverage as complete (no pending embedding work) instead of a bare "no statistics yet" empty state

#### Scenario: No queue statistics and no coverage

- **WHEN** the embeddings status page loads and every queue count is zero
- **AND** coverage is unavailable or does not report any live rows
- **THEN** a clear "no statistics yet" empty state is shown

#### Scenario: Stale-sweep failures are not presented as current failures

- **WHEN** a queue contains terminal jobs whose error message is the stale-job sweep marker and contains no genuine failures
- **THEN** the failed count is zero
- **AND** the stale-failed count shows the stale-sweep terminal rows

#### Scenario: A queue with only stale failures is not treated as empty

- **WHEN** the embeddings status page loads and every queue count is zero except stale-failed
- **THEN** the queue statistics are shown (the page does not render the empty state)
