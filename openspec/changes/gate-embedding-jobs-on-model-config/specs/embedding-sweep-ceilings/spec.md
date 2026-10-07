## Purpose

Tighten sweep admission so it refuses projects that are blocked or whose effective embedding model is
unresolvable — not merely projects whose `embedding_model` string is empty — and stop re-admitting a terminal
job's object every sweep while the project is blocked.

## MODIFIED Requirements

### Requirement: Object admission is keyed to available queue depth

The sweep worker SHALL admit objects into `kb.graph_embedding_jobs` in proportion to available queue capacity,
not a fixed per-sweep constant. Admission per sweep SHALL be `max(0, ObjectQueueTargetDepth − active)` where
`active` counts `pending`|`processing` graph embedding jobs. If the active-job count cannot be read, the sweep
SHALL admit nothing for that sweep rather than risk a flood. Admission SHALL remain idempotent: an object with
an active job, or a project that is blocked or whose effective embedding model is unresolvable, SHALL NOT be
enqueued. Resolvability SHALL be determined with the same effective-model resolution as the enqueue gate
(`modelconfig.Service.ResolveEmbeddingModel`), not merely `embedding_model != ''`.

#### Scenario: Admission is not capped by the constant batch size

- **WHEN** the target depth is 500, the queue is empty, and 600 objects lack embeddings
- **THEN** the sweep enqueues 500 objects (not the constant 200), and 100 remain for a later sweep

#### Scenario: A full queue is not flooded

- **WHEN** the active job count already equals the target depth
- **THEN** the sweep enqueues nothing

#### Scenario: Active jobs reduce admission

- **WHEN** the target depth is 10 and 6 jobs are already active
- **THEN** the sweep admits at most 4 objects

#### Scenario: A failed depth read admits nothing

- **WHEN** the active-job count query fails
- **THEN** the sweep enqueues no objects for that sweep

#### Scenario: A blocked project is not enqueued

- **WHEN** the sweep evaluates objects for a project that is blocked
- **THEN** the sweep admits nothing for that project

#### Scenario: An unresolvable model is not enqueued

- **WHEN** a project's effective embedding model does not resolve (misconfigured provider credential, not merely an empty `embedding_model` string)
- **THEN** the sweep admits nothing for that project

## ADDED Requirements

### Requirement: A terminal job's object is not re-admitted while its project is blocked

An object whose embedding job reached a terminal state (failed or dead-letter) SHALL NOT be re-admitted by the
sweep on every pass while its project is blocked. The sweep SHALL skip blocked projects, so their terminal
jobs are not churned into fresh `pending` jobs each sweep.

#### Scenario: Blocked project's terminal job is not churned

- **WHEN** a project is blocked and an object's embedding job is terminal
- **THEN** the sweep does not enqueue a fresh job for that object on each sweep
