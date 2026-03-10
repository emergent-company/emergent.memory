## ADDED Requirements

### Requirement: Agent run attribution on usage events
The system SHALL record the originating agent run ID on every `LLMUsageEvent` emitted during an agent execution so that token costs can be attributed to a specific run.

#### Scenario: Usage event stamped with run ID during agent execution
- **WHEN** an LLM generation call completes inside an active agent run
- **THEN** the resulting `llm_usage_events` row SHALL have its `run_id` column set to the ID of that agent run
- **AND** the run ID SHALL be propagated via a context key set by the agent executor before model creation

#### Scenario: Usage event outside an agent run leaves run_id null
- **WHEN** an LLM operation completes outside of an agent run (e.g., during extraction or embedding)
- **THEN** the resulting `llm_usage_events` row SHALL have `run_id` set to NULL
- **AND** the system SHALL NOT error or drop the event due to the absence of a run ID

#### Scenario: Migration adds nullable FK column
- **WHEN** migration `00050` is applied
- **THEN** the `kb.llm_usage_events` table SHALL gain a nullable `run_id uuid` column with a foreign key reference to `kb.agent_runs(id) ON DELETE SET NULL`
- **AND** a partial index on `run_id` (WHERE run_id IS NOT NULL) SHALL be created to support efficient per-run aggregation

### Requirement: Per-run token and cost aggregation
The system SHALL provide aggregated token counts and estimated cost for a specific agent run via the existing agent run API endpoint.

#### Scenario: Token summary included in agent run DTO
- **WHEN** `GET /api/projects/:projectId/agent-runs/:runId` is called
- **AND** one or more `llm_usage_events` rows exist with that `run_id`
- **THEN** the response DTO SHALL include a `tokenUsage` object containing `totalInputTokens`, `totalOutputTokens`, and `estimatedCostUsd`
- **AND** `totalInputTokens` SHALL be the sum of `text_input_tokens + image_input_tokens + video_input_tokens + audio_input_tokens` across all attributed events

#### Scenario: Token summary is null when no LLM calls were made
- **WHEN** an agent run completed without making any LLM calls
- **THEN** the `tokenUsage` field in the response DTO SHALL be `null` (omitted from JSON via `omitempty`)
- **AND** this SHALL be distinguishable from a run whose usage events exist but carry no `run_id` attribution
