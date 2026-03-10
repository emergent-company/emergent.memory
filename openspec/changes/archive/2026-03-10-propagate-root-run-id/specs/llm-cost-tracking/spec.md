## MODIFIED Requirements

### Requirement: Intercepting LLM Usage Events
The system SHALL intercept and log multi-modal token usage (text, image, video, audio input counts, and output counts) for every LLM operation performed on behalf of a project. Each recorded event SHALL include a nullable `root_run_id` field carrying the top-level orchestration run ID when execution occurs within an agent run.

#### Scenario: Logging a successful LLM operation
- **WHEN** an LLM operation (e.g., embedding or generation) completes successfully
- **THEN** the system SHALL record an event in the `llm_usage_events` table
- **AND** the event SHALL include the project ID, provider name, model used, and token counts broken down by modality

#### Scenario: LLM usage event carries root_run_id when inside an orchestration
- **WHEN** an LLM operation completes during an agent run that is part of an orchestration tree
- **THEN** the recorded `llm_usage_events` row SHALL have `root_run_id` set to the top-level run's ID
- **AND** `run_id` SHALL remain the immediate run's own ID (unchanged)

#### Scenario: LLM usage event has null root_run_id outside agent execution
- **WHEN** an LLM operation completes outside of any agent run context (e.g., a background job or chat)
- **THEN** the `root_run_id` column SHALL be NULL for that event
- **AND** the event SHALL still be recorded with all other fields populated

#### Scenario: Cost for a full orchestration aggregatable by root_run_id
- **WHEN** a top-level agent run spawns sub-agents that each make multiple LLM calls
- **THEN** `SELECT SUM(estimated_cost_usd) FROM kb.llm_usage_events WHERE root_run_id = '<top_level_run_id>'` SHALL return the total cost for the entire orchestration
