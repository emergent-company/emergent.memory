## ADDED Requirements

### Requirement: Memory decay job reduces confidence for non-recalled memories
The system SHALL run a scheduled decay job that reduces the `confidence` property of Memory objects that have not been recalled within a configurable staleness threshold.

#### Scenario: Memory not recalled within staleness threshold receives confidence decay
- **WHEN** the decay job runs
- **AND** a Memory object has `last_used` older than the staleness threshold (default: 30 days)
- **AND** the Memory's `confidence` is > 0
- **THEN** the system SHALL multiply the Memory's `confidence` by the decay rate (default: 0.95)
- **THEN** the system SHALL update the `confidence` property in place (no new version created — this is a non-semantic change)

#### Scenario: Recently recalled memory is not decayed
- **WHEN** the decay job runs
- **AND** a Memory object has `last_used` within the staleness threshold
- **THEN** the system SHALL NOT modify that Memory's `confidence`

#### Scenario: Memory with null last_used uses created_at for staleness calculation
- **WHEN** the decay job runs
- **AND** a Memory object has never been recalled (`last_used` is null)
- **AND** `created_at` is older than the staleness threshold
- **THEN** the system SHALL apply confidence decay as if `last_used = created_at`

### Requirement: Memories below confidence review threshold are flagged for review
When a Memory's confidence drops below the review threshold, the system SHALL flag it for user review.

#### Scenario: Memory flagged for review when confidence drops below threshold
- **WHEN** the decay job reduces a Memory's `confidence` below 0.3 (configurable)
- **AND** the Memory does not already have `needs_review = true`
- **THEN** the system SHALL set `needs_review = true` on the Memory object
- **THEN** the system SHALL NOT delete or archive the memory at this stage

#### Scenario: Recalled memory clears needs_review flag
- **WHEN** a Memory with `needs_review = true` is retrieved by `recall_memories`
- **THEN** the system SHALL clear `needs_review` (set to `false`)
- **THEN** the system SHALL restore `confidence` to 0.5 (configurable review recovery value)

### Requirement: Memories below archive threshold are auto-archived after grace period
Memories that remain below the archive confidence threshold for longer than the grace period SHALL be automatically soft-deleted.

#### Scenario: Memory auto-archived after grace period
- **WHEN** the decay job runs
- **AND** a Memory has `confidence < 0.1` (configurable archive floor)
- **AND** the Memory has had `needs_review = true` for longer than the grace period (default: 7 days)
- **AND** the Memory does NOT have `category = instruction` or `tier = core`
- **THEN** the system SHALL soft-delete the Memory object
- **THEN** the system SHALL log the auto-archival with the memory ID, final confidence, and age

#### Scenario: Instruction and core memories are exempt from auto-archival
- **WHEN** the decay job identifies a Memory below the archive floor
- **AND** the Memory has `category = instruction` OR `tier = core`
- **THEN** the system SHALL NOT auto-archive the memory
- **THEN** the system SHALL set `needs_review = true` regardless of current value
- **THEN** the system SHALL log: "Memory below archive floor but exempt from auto-archival (category=instruction|tier=core)"

#### Scenario: Memory recalled before grace period expires is not archived
- **WHEN** a Memory with `needs_review = true` and `confidence < 0.1` is recalled via `recall_memories`
- **THEN** the system SHALL clear `needs_review` and restore confidence (same as flagged memory recall)
- **THEN** the memory SHALL NOT be auto-archived in the next decay run

### Requirement: Memory decay job runs on a configurable schedule
The decay job SHALL run on a configurable schedule (default: weekly on Sunday at 03:00 UTC).

#### Scenario: Decay job runs on schedule
- **WHEN** the configured decay schedule triggers
- **THEN** the system SHALL process all active Memory objects across all users in the current organization
- **THEN** the job SHALL complete with a summary log: total processed, decayed, flagged, archived

#### Scenario: Decay job failure is recoverable
- **WHEN** the decay job fails mid-run
- **THEN** already-processed memories retain their updated confidence values
- **THEN** the job MAY be safely retried — the decay is idempotent within the same week (applying the same multiplier twice is bounded)

### Requirement: Decay configuration is operator-controllable
Decay parameters SHALL be configurable via environment variables or application config, without code changes.

#### Scenario: Operator changes staleness threshold
- **WHEN** the operator sets `MEMORY_DECAY_STALENESS_DAYS=60`
- **THEN** the decay job SHALL only apply decay to memories with `last_used` older than 60 days

#### Scenario: Operator disables auto-archival
- **WHEN** the operator sets `MEMORY_DECAY_AUTO_ARCHIVE_ENABLED=false`
- **THEN** the decay job SHALL apply confidence decay and set `needs_review` flags
- **THEN** the decay job SHALL NOT auto-archive any memories
