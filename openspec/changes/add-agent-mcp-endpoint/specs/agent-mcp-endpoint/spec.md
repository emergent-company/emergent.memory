## Purpose

Exposes a single configured agent as its own MCP server so any external LLM can call exactly one tool — `call_agent` — and receive the agent's reply, with no access to the rest of the project's tools.

## ADDED Requirements

### Requirement: Per-agent endpoint exposes exactly one tool

The system SHALL expose an MCP endpoint at `/api/mcp/agents/:agentId` that supports the standard `initialize`, `tools/list`, and `tools/call` methods. `tools/list` on this endpoint MUST return exactly one tool, named `call_agent`, whose description identifies the bound agent. No other project tool SHALL be listed or callable through this endpoint.

#### Scenario: Listing tools shows only call_agent

- **WHEN** an authorized client calls `tools/list` on the per-agent endpoint
- **THEN** the response contains exactly one tool named `call_agent`

#### Scenario: Project tools are not exposed

- **WHEN** an authorized client calls `tools/list` on the per-agent endpoint
- **THEN** no project tool (for example `entity-search` or `schema-list`) is present

#### Scenario: Call of an unknown tool is rejected

- **WHEN** an authorized client calls `tools/call` for a tool name other than `call_agent`
- **THEN** the system returns a method/tool-not-found error and executes nothing

### Requirement: call_agent runs the bound agent synchronously and returns its reply

`call_agent` SHALL accept a required `message` string argument, run the bound agent once, and return the assistant's reply text in the tool result. The call MUST NOT return until the run has completed or the run budget is exhausted.

#### Scenario: Message returns the agent reply

- **WHEN** an authorized client calls `call_agent` with a `message`
- **THEN** the bound agent runs and the tool result contains the assistant's reply text

#### Scenario: Missing message is rejected

- **WHEN** an authorized client calls `call_agent` without a `message` argument
- **THEN** the system returns an invalid-params error and starts no run

#### Scenario: Reply reflects the agent's response

- **WHEN** the agent produces an assistant message during the run
- **THEN** that message's text is returned as the tool result content

### Requirement: Each call is an independent, stateless run

Every `call_agent` invocation SHALL start a new run with no conversation continuity from previous calls, unless a later change introduces explicit session support. The endpoint MUST NOT require or retain client session state between calls for correctness.

#### Scenario: Two calls do not share conversation state

- **WHEN** a client calls `call_agent` twice with different messages
- **THEN** each call starts a new run and neither depends on the other's history

#### Scenario: Call without prior initialize session still works per protocol

- **WHEN** a client establishes a valid MCP session and then calls `call_agent`
- **THEN** the call runs and returns normally

### Requirement: Endpoint authenticates a credential bound to the agent

The endpoint SHALL require a valid API credential (`X-API-Key` or `Authorization: Bearer`). A request MUST be rejected unless the credential is actively bound to the agent named in the URL and the agent belongs to the credential's project. The other project's agents MUST NOT be reachable with a token bound to a different agent.

#### Scenario: Bound credential is accepted

- **WHEN** a client connects using a key created for that agent
- **THEN** the request is authorized and `call_agent` is available

#### Scenario: Unbound credential is rejected

- **WHEN** a client connects using a valid project credential that is not bound to the agent in the URL
- **THEN** the system rejects the request as unauthorized/forbidden and starts no run

#### Scenario: Credential for a different agent is rejected

- **WHEN** a client uses a key bound to agent A against the endpoint for agent B
- **THEN** the system rejects the request

#### Scenario: No credential is rejected

- **WHEN** a client calls the endpoint with no credential
- **THEN** the system returns an unauthorized response

### Requirement: The run budget is bounded

The system SHALL cap `call_agent` execution with a maximum step count and an execution timeout so that a call does not run unbounded. When the budget is exhausted, the system MUST return a structured error rather than hanging.

#### Scenario: Run exceeding the budget is stopped

- **WHEN** the agent would run beyond the configured step or time budget
- **THEN** execution stops and the tool result reports a budget/timeout error

#### Scenario: Normal run completes within the budget

- **WHEN** the agent completes within the budget
- **THEN** the reply is returned normally

### Requirement: Failures and pauses return structured tool errors

When the agent is missing, disabled, or not a member of the credential's project, or when the run fails or pauses for human input, `call_agent` SHALL return a structured tool error (`isError` true) with a clear message and MUST NOT return a misleading success.

#### Scenario: Missing or disabled agent

- **WHEN** the URL names an agent that does not exist, is disabled, or is not in the project
- **THEN** the tool result is an error explaining the agent is unavailable

#### Scenario: Run failure

- **WHEN** the agent run fails
- **THEN** the tool result is an error describing the failure

#### Scenario: Run pauses for human input

- **WHEN** the run pauses awaiting human input
- **THEN** the tool result is an error indicating the run needs input and no reply text is fabricated

### Requirement: Agent-share credentials are not valid on the project MCP endpoint

A credential minted for a per-agent share SHALL carry a dedicated marker scope (`mcp:agent-call`) and the project MCP transports (`/api/mcp`, `/api/mcp/rpc`, `/api/mcp/sse/...`) MUST reject any credential carrying that marker with HTTP 403 before listing or executing any tool. Normal project credentials (which do not carry the marker) MUST be unaffected. Conversely, the per-agent endpoint `/api/mcp/agents/:agentId` SHALL require the marker scope and reject credentials lacking it. This prevents a share key for agent A from reaching the project catalog and calling `trigger_agent` or other agent tools for a different agent B.

#### Scenario: Share credential rejected on the project endpoint

- **WHEN** a client uses a per-agent share key against `/api/mcp` (or its `/rpc`/`/sse` transports)
- **THEN** the system returns HTTP 403 and lists/executes no project tool

#### Scenario: Share credential accepted on its agent endpoint

- **WHEN** a client uses the same share key against `/api/mcp/agents/:agentId` for its bound agent
- **THEN** the request is authorized and only `call_agent` is available

#### Scenario: Normal project credential is unaffected

- **WHEN** a client uses a normal project key (without the marker) against `/api/mcp`
- **THEN** the request is not rejected by the agent-share rule

#### Scenario: Normal project credential rejected on the agent endpoint

- **WHEN** a client uses a normal project key against `/api/mcp/agents/:agentId`
- **THEN** the system returns HTTP 403

### Requirement: The reply contains only assistant text

The reply returned by `call_agent` SHALL be extracted exclusively from run messages whose raw role is `assistant`. System messages and tool results MUST NOT be returned as the reply, even though the ACP role mapping collapses them to `agent`. When a run completes without any non-empty assistant text, the system MUST return a structured error (`isError` true) and MUST NOT return an empty or fabricated success.

#### Scenario: System and tool_result text is never returned

- **WHEN** a run's messages include `system` or `tool_result` text but no assistant text
- **THEN** the tool result is a structured error and the reply is empty

#### Scenario: Assistant text is preferred over other roles

- **WHEN** a run's messages include system, user, tool_result, and assistant text
- **THEN** only the assistant text is returned

#### Scenario: No assistant reply is an error, not silent success

- **WHEN** a run completes successfully but persists no non-empty assistant message
- **THEN** the tool result is a structured error rather than an empty success
