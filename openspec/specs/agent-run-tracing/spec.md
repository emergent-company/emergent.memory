## ADDED Requirements

### Requirement: Agent run span carrying run ID
The AgentExecutor SHALL create an OTel span for each agent run. The span SHALL be a child of the HTTP request span (inheriting context from the handler). The span SHALL carry `emergent.agent.run_id` and `emergent.agent.id` as attributes so traces can be directly linked to the agent run browser.

#### Scenario: Agent run span created as child of HTTP span
- **WHEN** `POST /api/agents/:id/execute` is called and tracing is enabled
- **THEN** the trace SHALL contain an HTTP parent span AND a child span named `agent.run`
- **AND** the child span SHALL carry attributes: `emergent.agent.id`, `emergent.agent.run_id`, `emergent.project.id`

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

### Requirement: Agent run span ends on cancellation or timeout
If an agent run is cancelled or reaches `max_steps`, the span SHALL end with an appropriate status event rather than hanging open.

#### Scenario: Span ends on step limit reached
- **WHEN** an agent run reaches `max_steps`
- **THEN** the `agent.run` span SHALL add event `agent.max_steps_reached`
- **AND** the span status SHALL be set to `error` with message `max steps exceeded`

#### Scenario: Span ends on cancellation
- **WHEN** the agent run context is cancelled (e.g. server shutdown)
- **THEN** the `agent.run` span SHALL end immediately via `defer span.End()`
- **AND** the span status SHALL reflect the cancellation

### Requirement: No message content in agent spans
Agent run spans SHALL NOT include conversation messages, tool inputs, tool outputs, or any LLM-generated text as attributes. These are already stored in `kb.agent_run_messages` and `kb.agent_run_tool_calls`.

#### Scenario: Tool call result not in span
- **WHEN** an agent tool call completes during a run
- **THEN** the span attributes SHALL NOT include `tool.input` or `tool.output` content
- **AND** only `emergent.agent.step_count` SHALL be incremented
