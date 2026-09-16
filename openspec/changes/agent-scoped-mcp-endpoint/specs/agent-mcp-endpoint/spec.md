## Purpose

Exposes one configured agent as its own MCP server with a fixed minimal session tool set — one-shot `call_agent` plus an opt-in persistent session path — with no access to the rest of the project's tools and no per-endpoint tool picking.

## MODIFIED Requirements

### Requirement: Per-agent endpoint exposes exactly one tool

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

## ADDED Requirements

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
