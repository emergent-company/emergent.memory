## ADDED Requirements

### Requirement: Token and cost columns in memory traces list
The CLI `memory traces list` command SHALL display per-run token totals and estimated cost alongside each trace when the trace is linked to an agent run.

#### Scenario: Token columns populated for traced agent runs
- **WHEN** `memory traces list` is executed
- **AND** a trace's root span carries an `emergent.agent.run_id` attribute
- **THEN** the output table SHALL include `INPUT TOKENS`, `OUTPUT TOKENS`, and `EST. COST` columns populated from the agent run's `tokenUsage` data
- **AND** secondary API calls to fetch token data SHALL be made concurrently for all traces on the page

#### Scenario: Token columns show dash when run ID is absent
- **WHEN** a trace has no `emergent.agent.run_id` span attribute
- **THEN** the `INPUT TOKENS`, `OUTPUT TOKENS`, and `EST. COST` columns SHALL display `—`
- **AND** the command SHALL NOT error or exit non-zero

#### Scenario: Token columns show dash when token data is unavailable
- **WHEN** the agent run API returns a 404 or the run's `tokenUsage` is null
- **THEN** the affected columns SHALL display `—`
- **AND** the command SHALL complete successfully with exit code 0

### Requirement: Token summary block in memory traces get
The CLI `memory traces get <traceID>` command SHALL display a token and cost summary block before the span tree when the trace is linked to an agent run.

#### Scenario: Summary block shown for an attributed trace
- **WHEN** `memory traces get <traceID>` is executed
- **AND** the trace contains `emergent.agent.run_id` in a span attribute
- **AND** the agent run has token usage data
- **THEN** a summary line SHALL appear before the span tree in the format:
  `Tokens: <N> in / <N> out  Est. Cost: $X.XXXXXX`

#### Scenario: Summary block omitted when run ID is absent
- **WHEN** the trace has no `emergent.agent.run_id` span attribute
- **THEN** no token summary block SHALL be displayed
- **AND** the span tree SHALL render identically to the current behavior

#### Scenario: emergent.agent.run_id printed in span attribute list
- **WHEN** `memory traces get <traceID>` renders a span that carries `emergent.agent.run_id`
- **THEN** that attribute SHALL be included in the printed key/value attribute list for that span
