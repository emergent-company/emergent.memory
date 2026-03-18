## ADDED Requirements

### Requirement: save_memory invokes LLM merge decision when similar memories exist
When `save_memory` is called and at least one existing Memory object has cosine similarity ≥ 0.70 (distance ≤ 0.30) to the new memory content, the system SHALL retrieve the top-3 similar memories and invoke an LLM merge decision call before writing to the graph.

#### Scenario: New memory is unique — no similar memories found
- **WHEN** `save_memory` is called with content that has no existing memories within cosine distance 0.30
- **THEN** the system SHALL skip the LLM merge call and write the new Memory object directly
- **THEN** the system SHALL return action `ADD` with the new memory ID

#### Scenario: LLM decides ADD — new memory is distinct from similar ones
- **WHEN** `save_memory` is called and similar memories exist
- **AND** the LLM merge decision returns `action: ADD`
- **THEN** the system SHALL create the new Memory object alongside the existing ones
- **THEN** the system SHALL NOT modify the existing memories

#### Scenario: LLM decides UPDATE — new memory merges into an existing one
- **WHEN** `save_memory` is called and similar memories exist
- **AND** the LLM merge decision returns `action: UPDATE` with `target_id` and `merged_content`
- **THEN** the system SHALL create a new version of the target Memory object with `merged_content` as the updated content
- **THEN** the system SHALL create a `SUPERSEDES` relationship from the new version to the old version
- **THEN** the system SHALL set `superseded_by` on the old memory to the new version ID
- **THEN** the system SHALL return the new memory ID

#### Scenario: LLM decides DELETE_OLD_ADD_NEW — contradiction detected
- **WHEN** `save_memory` is called and the LLM merge decision returns `action: DELETE_OLD_ADD_NEW` with `target_id` and `merged_content`
- **THEN** the system SHALL soft-delete the target Memory object
- **THEN** the system SHALL create a new Memory object with `merged_content`
- **THEN** the system SHALL return action `REPLACED` with old and new memory IDs

#### Scenario: LLM decides NOOP — new memory is redundant
- **WHEN** `save_memory` is called and the LLM merge decision returns `action: NOOP`
- **THEN** the system SHALL NOT write any new Memory object
- **THEN** the system SHALL return action `NOOP` with the ID of the existing memory that made it redundant

### Requirement: Memories with source corrected are protected from merge deletion
Memories with `source = corrected` (explicitly corrected by the user) SHALL NOT be targeted for deletion by the LLM merge decision.

#### Scenario: LLM attempts to delete a corrected memory
- **WHEN** the LLM merge decision returns `action: DELETE_OLD_ADD_NEW` targeting a memory with `source = corrected`
- **THEN** the system SHALL override the decision to `ADD` instead
- **THEN** the system SHALL log the override with reason "protected: source=corrected"
- **THEN** the system SHALL create the new memory alongside the corrected memory

### Requirement: LLM merge decisions are logged
Every LLM merge decision call SHALL be logged with its input content, similar memory IDs, decision action, and execution latency.

#### Scenario: Successful merge decision logged
- **WHEN** the LLM merge decision call completes successfully
- **THEN** the system SHALL write a log entry containing: new content hash, top-3 similar memory IDs, decision action, target_id (if applicable), and latency in ms

#### Scenario: Failed merge decision falls back to threshold behavior
- **WHEN** the LLM merge decision call fails (timeout, error, invalid output)
- **THEN** the system SHALL fall back to the original threshold-based behavior (supersede if similarity ≥ 0.85, else add)
- **THEN** the system SHALL log the failure and fallback action
