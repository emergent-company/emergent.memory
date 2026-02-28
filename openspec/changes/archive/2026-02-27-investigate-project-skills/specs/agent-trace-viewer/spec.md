## ADDED Requirements

### Requirement: Trace Investigation
The agent SHALL be able to list and retrieve OpenTelemetry traces using the emergent CLI.

#### Scenario: Listing Traces
- **WHEN** the user asks to see recent traces
- **THEN** the agent runs `task traces:list`

#### Scenario: Fetching a Specific Trace
- **WHEN** the user asks to get details for a specific trace ID
- **THEN** the agent runs `task traces:get <traceID>`
