# embedding-status Specification

## Purpose
Surfaces embedding-generation status and progress so users can see whether knowledge-graph objects and relationships have been embedded and whether the embedding workers are healthy.

## Requirements

### Requirement: Navigate to the embeddings status page

The app SHALL provide a navigation entry that opens the embeddings status page.

#### Scenario: Open from navigation

- **WHEN** a user selects the embeddings entry in the app navigation
- **THEN** the embeddings status page opens

### Requirement: Show embedding queue progress

The embeddings status page SHALL show, for both objects and relationships, the count of embedding jobs in each state (pending, processing, completed, failed, dead-letter).

#### Scenario: Stats present

- **WHEN** the embeddings status page loads and queue statistics are available
- **THEN** pending, processing, completed, failed, and dead-letter counts are shown for objects and for relationships

#### Scenario: No statistics available

- **WHEN** the embeddings status page loads and no queue statistics are available
- **THEN** a clear "no statistics yet" empty state is shown

### Requirement: Show embedding worker state

The embeddings status page SHALL show whether the object, relationship, and sweep embedding workers are running or paused, and SHALL show the active worker configuration.

#### Scenario: Worker state present

- **WHEN** the embeddings status page loads
- **THEN** the running/paused state of the object, relationship, and sweep workers is shown

#### Scenario: Configuration present

- **WHEN** the embeddings status page loads
- **THEN** the active worker configuration (batch size, concurrency, polling interval, stale threshold) is shown

### Requirement: Surface load failures without crashing

The embeddings status page SHALL show a clear error state when a backend fetch fails and SHALL NOT render a broken page.

#### Scenario: Backend unreachable

- **WHEN** a required backend request fails
- **THEN** the affected section shows an error message while the rest of the page remains usable

### Requirement: Warn when no embedding model is configured

The embeddings status page SHALL surface a warning when the active project has no embedding model — resolved project-first, then provider-credential fallback — because in that state objects and relationships never get vectors and similar-objects search stays incomplete.

#### Scenario: No embedding model configured

- **WHEN** the embeddings status page loads for a project with no embedding model on the project config or any provider credential
- **THEN** a warning is shown stating embeddings will not be generated
- **AND** the warning links to the project provider settings so the user can configure one

#### Scenario: Embedding model configured

- **WHEN** the embeddings status page loads for a project whose embedding model is set on the project config or a provider credential
- **THEN** no missing-model warning is shown

#### Scenario: Model config fetch fails

- **WHEN** the effective model config fetch fails
- **THEN** the page renders without the warning (state treated as unknown, never blanks the page)
