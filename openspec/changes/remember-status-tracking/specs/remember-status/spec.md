## Purpose

Gives callers of async `remember`/`forget` operations a way to check whether the background run has finished and what graph objects/relationships it created, updated, or failed to create, without needing to poll unrelated run-execution status alone.

## ADDED Requirements

### Requirement: remember-status tool reports run completion state
The system SHALL expose a `remember-status` MCP tool (and equivalent REST route) that, given a `run_id`, returns the current lifecycle status of that run: `running`, `completed`, or `failed`.

#### Scenario: Run still in progress
- **WHEN** a caller invokes `remember-status` with a `run_id` for a run that has not finished
- **THEN** the response SHALL include `"status": "running"`
- **THEN** the response SHALL NOT claim any objects or relationships as final (counts MAY be reported as partial/in-progress or omitted)

#### Scenario: Queued extraction still in progress
- **WHEN** a caller invokes `remember-status` with a `run_id` whose agent run has already finished, but the run queued one or more `queue-reextraction` extraction jobs that are still `pending` or `processing`
- **THEN** the response SHALL include `"status": "running"` (remembering is not done until the extraction jobs that perform the graph mutations finish)
- **THEN** the response SHALL include `"partial": true` and MAY report counts aggregated from whatever has completed so far

#### Scenario: Run completed successfully
- **WHEN** a caller invokes `remember-status` with a `run_id` for a run that finished without error
- **THEN** the response SHALL include `"status": "completed"`

#### Scenario: Run failed
- **WHEN** a caller invokes `remember-status` with a `run_id` for a run that terminated with an error
- **THEN** the response SHALL include `"status": "failed"`
- **THEN** the response SHALL include an `error` field with a human-readable failure reason

#### Scenario: Unknown run_id
- **WHEN** a caller invokes `remember-status` with a `run_id` that does not exist or does not belong to the caller's project
- **THEN** the system SHALL return an error result indicating the run was not found
- **THEN** the system SHALL NOT leak information about runs belonging to other projects

### Requirement: remember-status reports graph mutation summary
When a run has produced at least one graph-mutating tool call, `remember-status` SHALL report a summary of what was created, updated, or attempted, derived from that run's recorded tool calls. Because the primary `remember` flow mutates the graph through an async `queue-reextraction` extraction job rather than direct tool calls, the summary SHALL also include the results of any extraction jobs the run queued (followed via `job_id`), merging them into the same counts.

#### Scenario: Objects and relationships created
- **WHEN** a completed run's tool calls include successful `entity-create`, `entity-update`, `entity-relationship-create`, or note-creation calls
- **THEN** the response SHALL include `objects_created` and `objects_updated` counts
- **THEN** the response SHALL include a `relationships_created` count
- **THEN** the response SHALL include a list of created object/relationship identifiers when available from the tool call output

#### Scenario: Objects created by a queued extraction job
- **WHEN** a completed run queued one or more `queue-reextraction` extraction jobs that have since completed
- **THEN** the response SHALL include the jobs' created-object/relationship counts and identifiers merged into the same `objects_created`, `relationships_created`, `created_object_ids`, and `discovered_types` fields
- **THEN** if any queued extraction job failed or dead-lettered for a reason other than the stale-job sweep, the response SHALL surface that in the `error` field, and the overall `status` SHALL be `failed` even if the agent run itself completed (the requested graph mutations never happened)

#### Scenario: Stale-swept extraction jobs are reported separately, not as failures
- **WHEN** a queued extraction job's terminal status is `failed` or `dead_letter` and its error text equals the canonical stale-sweep marker (`jobs.StaleJobMessage`, the same value written by `domain/scheduler.StaleJobCleanupTask`)
- **THEN** the response SHALL NOT include that job in the `error` field or the extraction failure list, and the overall `status` SHALL NOT be forced to `failed` by it
- **THEN** the response SHALL include a `stale_jobs` integer count of such reaped jobs (always present, `0` when none), so the reap remains visible rather than being dropped
- **THEN** genuine failures and dead-letters SHALL continue to fail the run as before

#### Scenario: Discovered types are surfaced
- **WHEN** a completed run created entities or relationships of one or more types
- **THEN** the response SHALL include a `discovered_types` list of the distinct types touched

#### Scenario: No graph mutations occurred
- **WHEN** a completed run made no graph-mutating tool calls (e.g., it determined nothing new was worth saving)
- **THEN** the response SHALL report zero counts rather than omitting the fields
- **THEN** the response SHALL include a `summary` string indicating no changes were made

#### Scenario: Partial failures within a run
- **WHEN** some graph-mutating tool calls within the run failed while others succeeded
- **THEN** the reported counts SHALL reflect only successful mutations
- **THEN** the response MAY include a note that some operations failed, without failing the overall status if the run itself completed

### Requirement: remember-status reports embedding readiness
`remember-status` SHALL report whether the objects a run created are ready for semantic recall/search, based on their `kb.graph_embedding_jobs` status, without changing the overall `status` field.

#### Scenario: Embeddings still processing
- **WHEN** a run's created objects have embedding jobs that are still `pending` or `processing`
- **THEN** the response SHALL include `embeddings_pending` equal to the number of created objects whose embedding is not yet done
- **THEN** the response SHALL include `"embeddings_ready": false`
- **THEN** the overall `status` SHALL remain `"completed"` (graph mutation succeeded) — embedding readiness is a separate signal callers can poll on for full recall-readiness

#### Scenario: Embeddings completed
- **WHEN** all of a run's created objects have completed embedding jobs (or no embedding job exists for them)
- **THEN** the response SHALL include `"embeddings_pending": 0` and `"embeddings_ready": true`

#### Scenario: Embedding generation failed
- **WHEN** one or more of a run's created objects have failed or dead-lettered embedding jobs for a reason other than the stale-job sweep
- **THEN** the response SHALL include `embeddings_failed` equal to the number of affected objects
- **THEN** the response SHALL include a summary note that recall may miss those objects
- **THEN** the overall `status` SHALL remain `"completed"` (embedding failures do not fail the memorize operation)

#### Scenario: Stale-swept embedding jobs are reported separately
- **WHEN** one or more of a run's created objects have embedding jobs whose error text equals the canonical stale-sweep marker (`jobs.StaleJobMessage`)
- **THEN** the response SHALL count them in an `embeddings_stale` integer rather than in `embeddings_failed`
- **THEN** the overall `status` SHALL remain `"completed"`

#### Scenario: Embedding tracking unavailable
- **WHEN** the embedding job finder is not configured
- **THEN** the system SHALL log a warning and omit the embedding fields rather than erroring or reporting them as zero

### Requirement: remember-status is scoped to the caller's project
`remember-status` SHALL only report on runs that belong to the authenticated caller's project.

#### Scenario: Cross-project access denied
- **WHEN** a caller supplies a `run_id` belonging to a different project
- **THEN** the system SHALL treat it identically to a not-found run_id
