## ADDED Requirements

### Requirement: Memory objects support bi-temporal tracking
The `Memory` object type SHALL support an optional `event_time` property (ISO 8601 string) representing when the described event occurred, distinct from `created_at` (ingestion time).

#### Scenario: Memory saved with explicit event_time
- **WHEN** `save_memory` is called with an `event_time` parameter
- **THEN** the created Memory object SHALL store `event_time` in its JSONB properties
- **WHEN** the memory is recalled
- **THEN** the response SHALL include both `event_time` and `created_at`

#### Scenario: Memory saved without event_time defaults to ingestion time
- **WHEN** `save_memory` is called without an `event_time` parameter
- **THEN** the Memory object SHALL be created with `event_time = null`
- **THEN** query-time decay SHALL use `created_at` as the reference timestamp

### Requirement: save_memory normalizes relative date expressions to absolute ISO timestamps
When memory content contains relative temporal expressions ("yesterday", "last week", "two months ago"), the system SHALL normalize them to absolute ISO 8601 timestamps before storage.

#### Scenario: Relative date in content normalized at ingestion
- **WHEN** `save_memory` is called with content containing "yesterday"
- **AND** the current date is 2026-03-18
- **THEN** the stored `event_time` SHALL be set to 2026-03-17T00:00:00Z (or the inferred date)
- **THEN** the stored `content` SHALL preserve the original phrasing

#### Scenario: Absolute date passes through unchanged
- **WHEN** `save_memory` is called with content referencing "2025-06-15"
- **THEN** `event_time` SHALL be set to 2025-06-15T00:00:00Z without modification

### Requirement: LLM merge decision uses event_time for temporal contradiction resolution
When both the candidate memory and a similar existing memory have `event_time` set, the merge decision prompt SHALL receive both timestamps and use them to inform contradiction resolution.

#### Scenario: More recent event supersedes older event
- **WHEN** existing memory has `event_time = 2023-01-01` ("User lives in New York")
- **AND** new memory has `event_time = 2026-01-01` ("User lives in London")
- **THEN** the LLM merge decision SHALL resolve as `DELETE_OLD_ADD_NEW` (more recent event wins)
- **THEN** the new memory SHALL record the old memory's ID in a `supersedes` property

#### Scenario: Missing event_time falls back to content-based merge decision
- **WHEN** neither the candidate nor the existing memory has `event_time` set
- **THEN** the merge decision SHALL proceed using content semantics only (no temporal bias)

### Requirement: recall_memories applies exponential score decay at query time
During retrieval, the final ranking score for each memory SHALL be computed as:

`S_final = S_semantic * exp(-λ * age_days)`

Where `age_days` is computed from `event_time` (if present) or `created_at`, and `λ` is a per-category configurable decay constant.

#### Scenario: Older memory of same semantic similarity ranks lower
- **WHEN** two memories have identical semantic similarity to a query
- **AND** memory A has `age_days = 10` and memory B has `age_days = 365`
- **THEN** memory A SHALL rank higher in the response after score decay is applied

#### Scenario: Decay is skipped for category=instruction memories
- **WHEN** a memory has `category = instruction`
- **THEN** the decay multiplier SHALL be 1.0 (no decay applied)
- **THEN** the memory's ranking SHALL be based on semantic similarity only

#### Scenario: Per-category lambda overrides are applied
- **WHEN** `MEMORY_DECAY_LAMBDA_CORRECTION=0.0005` is configured
- **AND** a memory has `category = correction` and `age_days = 100`
- **THEN** the score multiplier SHALL be `exp(-0.0005 * 100) ≈ 0.951`
