## ADDED Requirements

### Requirement: Intercepting LLM Usage Events
The system SHALL intercept and log multi-modal token usage (text, image, video, audio input counts, and output counts) for every LLM operation performed on behalf of a project.

#### Scenario: Logging a successful LLM operation
- **WHEN** an LLM operation (e.g., embedding or generation) completes successfully
- **THEN** the system SHALL record an event in the `llm_usage_events` table
- **AND** the event SHALL include the project ID, provider name, model used, and token counts broken down by modality

### Requirement: Estimating Multi-modal Costs Based on Provider Pricing
The system SHALL calculate an estimated cost for each LLM usage event by resolving pricing through a defined hierarchy: `organization_custom_pricing` -> `provider_pricing`.

#### Scenario: Calculating estimated costs with retail pricing
- **WHEN** the system records an LLM usage event
- **AND** there is no custom pricing defined for the organization
- **THEN** it SHALL look up the price per 1k tokens for the requested modalities from the global `provider_pricing` table
- **AND** it SHALL compute the total estimated cost in USD based on the specific media types processed
- **AND** it SHALL persist the estimated cost alongside the usage event in `llm_usage_events`

#### Scenario: Calculating estimated costs with organization overrides
- **WHEN** the system records an LLM usage event
- **AND** the organization has a row defined in the `organization_custom_pricing` table for the specific model used
- **THEN** it SHALL calculate the estimated cost using the custom enterprise rates defined in that specific tenant's override table, ignoring the global retail price.

### Requirement: Dynamic Pricing Synchronization
The system SHALL periodically sync the retail pricing of supported models from an external registry to ensure global cost estimates remain accurate as provider prices drop.

#### Scenario: Running the daily pricing sync
- **WHEN** the daily background pricing cron job executes
- **THEN** it SHALL fetch the latest multi-modal pricing from the configured external registry (e.g., `models.dev` or a dedicated JSON URL)
- **AND** it SHALL update the prices and the `last_synced` timestamp in the global `provider_pricing` table

#### Scenario: Ensuring enterprise rates are untouched by sync
- **WHEN** the global daily background pricing cron job executes
- **THEN** it SHALL NOT modify or interact with the `organization_custom_pricing` table, preserving all negotiated enterprise rates for individual tenants

### Requirement: Exposing Usage and Cost Summary via API and CLI
The system SHALL provide API endpoints and CLI commands to fetch aggregate token usage and cost summaries by project and provider over a specified time period.

#### Scenario: Fetching project cost summary
- **WHEN** an administrator requests the cost summary for a project via the API or CLI
- **THEN** the system SHALL return the total tokens, and aggregated cost estimate grouped by provider and model
- **AND** the CLI and API MUST clearly label the resulting financial value as an "Estimated Cost" to avoid confusion with actual provider invoices