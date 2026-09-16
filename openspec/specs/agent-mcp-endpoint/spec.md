# agent-mcp-endpoint Specification

## Purpose
Exposes one configured agent as its own MCP server with a fixed minimal session tool set — one-shot `call_agent` plus an opt-in persistent session path — with no access to the rest of the project's tools and no per-endpoint tool picking.

## Requirements

### Requirement: Per-agent endpoint exposes a fixed tool catalog

The system SHALL expose an MCP endpoint at `/api/mcp/agents/:agentId` that supports the standard `initialize`, `tools/list`, and `tools/call` methods. `tools/list` on this endpoint MUST return a fixed catalog of exactly five tools — `call_agent`, `start_session`, `continue_session`, `get_session`, and `list_sessions` — whose descriptions identify the bound agent. The catalog MUST be fixed: there is no per-endpoint tool picking, and the agent configures its own internal tools. No other project tool SHALL be listed or callable through this endpoint.

#### Scenario: Listing tools shows the fixed session catalog

- **WHEN** an authorized client calls `tools/list` on the per-agent endpoint
- **THEN** the response contains exactly `call_agent`, `start_session`, `continue_session`, `get_session`, and `list_sessions`

#### Scenario: Project tools are not exposed

- **WHEN** an authorized client calls `tools/list` on the per-agent endpoint
- **THEN** no project tool (for example `entity-search` or `schema-list`) is present

#### Scenario: The catalog is not configurable per endpoint

- **WHEN** an endpoint is created or used
- **THEN** no request, column, or configuration can add or remove a tool from the five-tool catalog

#### Scenario: Call of an unknown tool is rejected

- **WHEN** an authorized client calls `tools/call` for a tool name other than the five catalog tools
- **THEN** the system returns a method/tool-not-found error and executes nothing

### Requirement: call_agent runs the bound agent synchronously and returns its reply

`call_agent` SHALL accept a required `message` string argument, run the bound agent once, and return the agent's reply text in the tool result. The call MUST NOT return until the run has completed or the run budget is exhausted.

#### Scenario: Message returns the agent reply

- **WHEN** an authorized client calls `call_agent` with a `message`
- **THEN** the bound agent runs and the tool result contains the agent's reply text

#### Scenario: Missing message is rejected

- **WHEN** an authorized client calls `call_agent` without a `message` argument
- **THEN** the system returns an invalid-params error and starts no run

#### Scenario: Reply reflects the agent's response

- **WHEN** the agent produces an agent-authored message during the run
- **THEN** that message's text is returned as the tool result content

### Requirement: Each call is an independent, stateless run

`call_agent` SHALL remain stateless: every invocation MUST start a new run with no conversation continuity from previous calls, and the endpoint MUST NOT require or retain client session state for `call_agent` correctness. The session tools (`start_session`, `continue_session`) SHALL provide opt-in continuity scoped to the authorizing key, and MUST NOT change `call_agent` behavior or its reply shape.

#### Scenario: Two calls do not share conversation state

- **WHEN** a client calls `call_agent` twice with different messages
- **THEN** each call starts a new run and neither depends on the other's history

#### Scenario: Stateless and session paths coexist

- **WHEN** a client calls `call_agent` on an endpoint that also has active sessions
- **THEN** the `call_agent` run is independent of every session's history

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
- **THEN** the request is authorized and the session catalog is available

#### Scenario: Normal project credential is unaffected

- **WHEN** a client uses a normal project key (without the marker) against `/api/mcp`
- **THEN** the request is not rejected by the agent-share rule

#### Scenario: Normal project credential rejected on the agent endpoint

- **WHEN** a client uses a normal project key against `/api/mcp/agents/:agentId`
- **THEN** the system returns HTTP 403

### Requirement: The reply contains only agent-authored text

The reply returned by `call_agent` SHALL be extracted from the run's persisted agent-authored messages. The executor persists assistant turns under the ADK event Author — the sanitized agent name, for example `research_agent` — rather than the literal `assistant`, so the extractor MUST accept any raw role that is not an explicit `user`, `tool`, `tool_result`, or `system` side. System messages and tool results MUST NOT be returned as the reply, even though the ACP role mapping collapses them to `agent`. When a run completes without any non-empty agent-authored text, the system MUST return a structured error (`isError` true) and MUST NOT return an empty or fabricated success.

#### Scenario: System and tool output is never returned

- **WHEN** a run's messages include `system`, `tool`, or `tool_result` text but no agent-authored text
- **THEN** the tool result is a structured error and the reply is empty

#### Scenario: Agent-authored text is preferred over other roles

- **WHEN** a run's messages include system, user, tool, tool_result, and agent-authored text
- **THEN** only the agent-authored text is returned

#### Scenario: The sanitized agent name role is accepted

- **WHEN** the agent's reply is persisted under the sanitized agent name role (the executor's real behavior)
- **THEN** that text is returned rather than a "no assistant reply" error

#### Scenario: No agent reply is an error, not silent success

- **WHEN** a run completes successfully but persists no non-empty agent-authored message
- **THEN** the tool result is a structured error rather than an empty success

### Requirement: Session tools return the uniform envelope while call_agent stays bare

The session tools `start_session`, `continue_session`, `get_session`, and `list_sessions` SHALL return the uniform MCP result envelope `{ ok, error, data, meta }` defined by the `mcp-tool-results` contract, with the session payload in `data` and any failure carrying `ok:false`, a non-empty `error`, and `meta.kind`. `call_agent` MUST NOT be re-enveloped: it MUST keep returning the unchanged bare text reply.

#### Scenario: Session tool success is an ok envelope

- **WHEN** an authorized client calls a session tool that succeeds
- **THEN** the result is a single JSON object with `ok:true` and the session payload inside `data`

#### Scenario: Session tool failure carries a kind

- **WHEN** a session tool fails because the agent is unavailable, the run failed, human input is required, or the budget was exceeded
- **THEN** the result has `ok:false`, a non-empty `error`, and a `meta.kind` naming the failure

#### Scenario: call_agent remains un-enveloped

- **WHEN** an authorized client calls `call_agent` with a `message`
- **THEN** the result is the same bare text reply as before this change

### Requirement: Endpoint authorization resolves an active key bound to the agent

The endpoint SHALL authorize a request by resolving the active key bound to the presented API token and then its active endpoint, and MUST reject the request when the key is unknown/revoked/expired (403 "credential not bound"), when the token is revoked or expired (403), or when the endpoint's agent differs from the agent in the URL (403). The endpoint MUST additionally resolve the bound agent and reject the request with 403 when the agent is disabled, before any run starts. The credential's identity for sessions SHALL be the key, while the authorization target SHALL be the agent.

#### Scenario: Bound key is accepted

- **WHEN** a client connects using a key created for that agent endpoint
- **THEN** the request is authorized and the session catalog is available

#### Scenario: Unbound credential is rejected

- **WHEN** a client connects using a valid project credential that is not bound to any key for the agent in the URL
- **THEN** the system rejects the request as forbidden and starts no run

#### Scenario: Credential for a different agent is rejected

- **WHEN** a client uses a key whose endpoint binds agent A against the endpoint for agent B
- **THEN** the system rejects the request

#### Scenario: Disabled agent fails fast

- **WHEN** a client connects to an endpoint whose agent is disabled
- **THEN** the system rejects the request with 403 before any run starts

#### Scenario: Revoked or expired key is rejected

- **WHEN** a client connects using a key whose token is revoked or expired
- **THEN** the system rejects the request with 403
