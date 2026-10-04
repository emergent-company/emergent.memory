## Purpose

Records and exposes a per-parse-job stage timeline with progress, and lets an operator stop an in-flight parse through a real cancelled state so a stuck or unwanted job can be abandoned cleanly.

## ADDED Requirements

### Requirement: Record a parse stage timeline bound to the parse job

The server SHALL record a parse pipeline timeline for each parse job, consisting of the stages the parse passes through (for example download, extract, enrich, chunk, embed) with each stage's start time, end time, duration, status, and error when it fails. The timeline SHALL be bound to the parse job, and the document API SHALL resolve a document's timeline to its most recent parse job while retaining prior jobs' timelines.

#### Scenario: Timeline recorded for a parse job

- **WHEN** a document is parsed
- **THEN** the server records a timeline entry for each stage of that parse job with status and timing

#### Scenario: Failed stage recorded

- **WHEN** a parse stage fails
- **THEN** that stage's timeline entry records the failure and its error, and the stages that ran before it remain available

#### Scenario: Reparse does not erase the prior timeline

- **WHEN** a document is reparsed, creating a new parse job
- **THEN** the new job has its own timeline and the previous job's timeline remains retrievable

#### Scenario: Document resolves to the latest job

- **WHEN** a client requests a document's parse timeline after a reparse
- **THEN** the server returns the most recent parse job's timeline

### Requirement: Report parse progress

The server SHALL report a parse job's overall state and progress, including the current stage and the last completed stage.

#### Scenario: In-progress parse

- **WHEN** a parse is in progress
- **THEN** the server reports the current stage and that the parse is running

#### Scenario: Completed parse

- **WHEN** a parse has completed
- **THEN** the server reports the parse as succeeded or failed

### Requirement: Stop an in-flight parse

The server SHALL support a `cancelled` parse-job state and SHALL let an operator stop a running or queued parse for a document. Stopping SHALL mark the job cancelled, signal the in-flight parse to stop, and discard any derived artifacts of that attempt so the document is not left half-parsed. Stop SHALL be idempotent with a concurrently completing job: if the job completed before the stop was applied, the completed result SHALL be retained and the stop SHALL report that there was nothing to stop.

#### Scenario: Running parse stopped

- **WHEN** an operator stops a running parse
- **THEN** the job is marked cancelled, the in-flight parse is signalled to stop, and partial artifacts are discarded

#### Scenario: Stop races a completing parse

- **WHEN** a stop is applied while the parse completes concurrently
- **THEN** exactly one outcome is recorded, the completed result is never left half-applied, and the stop reports nothing to stop if completion won

#### Scenario: Stop a finished parse

- **WHEN** an operator stops a parse that has already finished
- **THEN** the server reports that there is nothing to stop and does not alter the completed result

### Requirement: Reparse from stored original bytes

The server SHALL let an operator reparse a document that has stored original bytes, using those bytes and an optional new parse configuration, without requiring a re-upload, and SHALL start a new parse job. For a document that participates in the versioning model, reparse SHALL update the current revision's derived content; creating a new revision remains the concern of the versioning model.

#### Scenario: Reparse with a different engine

- **WHEN** an operator reparses a document selecting a different engine
- **THEN** the server starts a new parse job using the stored original bytes and the selected engine

#### Scenario: Reparse requires original bytes

- **WHEN** a document has no stored original bytes (for example a raw-text document created from content)
- **THEN** the server rejects the reparse with a validation error

#### Scenario: Reparse of a versioned document updates the current revision

- **WHEN** a versioned document is reparsed
- **THEN** the current revision's derived content is updated and no new revision is created by the reparse
