## Purpose

Surface the project blocked state and reason on the embeddings status page, include blocked in the queue
counts, and broaden the "no embedding model" warning to cover misconfigured (unusable) models, not only
unset ones.

## MODIFIED Requirements

### Requirement: Show embedding queue progress

The embeddings status page SHALL show, for both objects and relationships, the count of embedding jobs in
each state (pending, processing, completed, failed, stale-failed, dead-letter). The `failed` count SHALL
include only genuine failures; jobs terminal-failed by the stale-job sweep SHALL be reported separately as
stale-failed and SHALL NOT be counted as failed. The page SHALL also surface blocked projects, and the counts
SHALL include the blocked state.

#### Scenario: Stats present

- **WHEN** the embeddings status page loads and queue statistics are available
- **THEN** pending, processing, completed, failed, stale-failed, and dead-letter counts are shown for objects and for relationships

#### Scenario: Blocked is included in the counts

- **WHEN** the embeddings status page loads and some projects are blocked
- **THEN** the blocked state is included in the queue statistics (the counts reflect blocked projects/objects rather than omitting them)

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

### Requirement: Warn when no embedding model is configured

The embeddings status page SHALL surface a warning when the active project has no *usable* embedding model —
resolved project-first, then provider-credential fallback — because in that state objects and relationships
never get vectors and similar-objects search stays incomplete. The warning SHALL cover a misconfigured model
(a provider credential that yields no resolvable embedding model), not only an unset one.

#### Scenario: No embedding model configured

- **WHEN** the embeddings status page loads for a project with no embedding model on the project config or any provider credential
- **THEN** a warning is shown stating embeddings will not be generated
- **AND** the warning links to the project provider settings so the user can configure one

#### Scenario: Misconfigured model is treated as unconfigured

- **WHEN** a project has a provider credential that yields no resolvable embedding model (misconfigured, not merely unset)
- **THEN** the "no embedding model" warning is shown, because the project cannot generate vectors

#### Scenario: Embedding model configured

- **WHEN** the embeddings status page loads for a project whose embedding model resolves on the project config or a provider credential
- **THEN** no missing-model warning is shown

#### Scenario: Model config fetch fails

- **WHEN** the effective model config fetch fails
- **THEN** the page renders without the warning (state treated as unknown, never blanks the page)

## ADDED Requirements

### Requirement: Surface the project blocked state and reason

The embeddings status page SHALL surface, for a blocked project, that embeddings are blocked and the machine-readable
reason (`embedding_blocked_reason`), so the user sees *why* nothing is embedding, not merely that the queues are stuck.

#### Scenario: Blocked project shows the reason

- **WHEN** the embeddings status page loads for a project that is blocked
- **THEN** the page shows that the project is blocked and the reason (for example `no_embedding_model` or `model_denied`)
