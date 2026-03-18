## ADDED Requirements

### Requirement: Memory reflection job clusters related memories and synthesizes MemoryContext summaries
The system SHALL run a scheduled memory reflection job that groups semantically related Memory objects into clusters and synthesizes a `MemoryContext` summary object for each cluster via an LLM call.

#### Scenario: Cluster of 3+ related memories synthesized into MemoryContext
- **WHEN** the reflection job runs
- **AND** 3 or more Memory objects for a given user have pairwise cosine distance ≤ 0.25 (similarity ≥ 0.75)
- **AND** none of the memories already belong to a `MemoryContext`
- **THEN** the system SHALL create a `MemoryContext` object with a synthesized `description` summarizing the cluster's common theme
- **THEN** the system SHALL set `confidence = 0.6` and `source = inferred` on the synthesized `MemoryContext`
- **THEN** the system SHALL create `BELONGS_TO_CONTEXT` relationships from each clustered Memory to the new `MemoryContext`

#### Scenario: Cluster of fewer than 3 memories is not synthesized
- **WHEN** the reflection job runs
- **AND** fewer than 3 Memory objects form a cluster
- **THEN** the system SHALL NOT create a `MemoryContext` for that cluster

#### Scenario: Existing MemoryContext updated when new memories join the cluster
- **WHEN** the reflection job runs
- **AND** new Memory objects have been added since the last run that belong to an existing cluster (cosine distance ≤ 0.25 to cluster centroid)
- **THEN** the system SHALL update the existing `MemoryContext` description to incorporate the new memories
- **THEN** the system SHALL create `BELONGS_TO_CONTEXT` relationships for the new memories
- **THEN** the system SHALL version the `MemoryContext` object (create new version, supersede old)

#### Scenario: Reflection job is idempotent
- **WHEN** the reflection job runs twice with no new memories added between runs
- **THEN** the second run SHALL produce no new `MemoryContext` objects or relationships
- **THEN** no duplicate clusters SHALL be created

### Requirement: Memory reflection job runs on a configurable schedule
The reflection job SHALL run on a configurable schedule (default: daily at 02:00 UTC) and SHALL process each user's memories independently.

#### Scenario: Reflection job scheduled and executed
- **WHEN** the configured reflection schedule triggers
- **THEN** the system SHALL enqueue a reflection job for each user who has ≥ 3 unclustered Memory objects
- **THEN** each job SHALL process that user's memories only
- **THEN** the job SHALL complete within a configurable timeout (default: 5 minutes per user)

#### Scenario: Reflection job failure does not affect other users
- **WHEN** the reflection job for user A fails
- **THEN** the system SHALL log the failure and mark the job as failed
- **THEN** the system SHALL continue processing other users' jobs
- **THEN** failed jobs SHALL be retried according to the standard scheduler retry policy

### Requirement: Synthesized MemoryContext objects are visible and manageable via manage_memory
Users and agents SHALL be able to view and delete synthesized `MemoryContext` objects via the `manage_memory` tool.

#### Scenario: List includes synthesized MemoryContext objects
- **WHEN** `manage_memory` is called with `action: list` and `type: context`
- **THEN** the response SHALL include all `MemoryContext` objects for the current user
- **THEN** each entry SHALL indicate whether the context was `source: inferred` (synthesized) or `source: explicit` (user-created)

#### Scenario: Delete synthesized MemoryContext removes relationships
- **WHEN** `manage_memory` is called with `action: delete` targeting a `MemoryContext` object
- **THEN** the system SHALL soft-delete the `MemoryContext` object
- **THEN** the system SHALL remove all `BELONGS_TO_CONTEXT` relationships from child memories to this context
- **THEN** the child Memory objects SHALL remain active and unmodified
