## MODIFIED Requirements

### Requirement: Agent run span carrying run ID
The AgentExecutor SHALL create an OTel span for each agent run. The span SHALL be a child of the HTTP request span (inheriting context from the handler). The span SHALL carry `emergent.agent.run_id`, `emergent.agent.id`, and `emergent.agent.root_run_id` as attributes so all spans in an orchestration tree are queryable by a single root identifier in Tempo/Jaeger.

#### Scenario: Agent run span created as child of HTTP span
- **WHEN** `POST /api/agents/:id/execute` is called and tracing is enabled
- **THEN** the trace SHALL contain an HTTP parent span AND a child span named `agent.run`
- **AND** the child span SHALL carry attributes: `emergent.agent.id`, `emergent.agent.run_id`, `emergent.project.id`, `emergent.agent.root_run_id`

#### Scenario: Run ID attribute enables browser deep-link
- **WHEN** a trace is viewed in the TUI trace detail panel
- **AND** a span with attribute `emergent.agent.run_id` is present
- **THEN** the TUI SHALL render the run ID as a navigable link showing the server URL path to that agent run
- **AND** the format SHALL be `<server-url>/agents/runs/<run_id>`

#### Scenario: Agent run span outlives HTTP response
- **WHEN** the agent executor starts an async run and the HTTP handler returns 202 Accepted
- **THEN** the `agent.run` span SHALL remain open until the agent run goroutine completes or errors
- **AND** the span SHALL end with `ok` status on success or `error` status with `error.message` on failure

#### Scenario: Agent span includes completion metadata
- **WHEN** an agent run completes successfully
- **THEN** the `agent.run` span SHALL include: `emergent.agent.step_count` (integer), `emergent.agent.run_status` (string: success/error/skipped)
- **AND** the span SHALL NOT include message content, tool inputs, or tool outputs

#### Scenario: root_run_id attribute is identical across all spans in one orchestration
- **WHEN** a top-level agent spawns one or more sub-agents (at any depth)
- **THEN** every `agent.run` span in that orchestration tree SHALL carry the same value for `emergent.agent.root_run_id`
- **AND** that value SHALL equal the top-level run's own `run_id`
