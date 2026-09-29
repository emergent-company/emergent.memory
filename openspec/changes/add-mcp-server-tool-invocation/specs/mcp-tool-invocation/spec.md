## ADDED Requirements

### Requirement: Invoke a single tool on an MCP server

The memory API SHALL expose a project-scoped endpoint
`POST /api/admin/mcp-servers/:id/tools/:toolName/call` that invokes exactly one cached tool on the
addressed server with caller-supplied JSON arguments and returns the tool's result. The request body
SHALL be `{"arguments": <JSON object>}`; an absent or empty body SHALL invoke the tool with no
arguments. The server SHALL be resolved by id within the caller's project, so a server belonging to
another project is indistinguishable from a missing one.

The endpoint SHALL require project membership (the same `RequireAuth` + `RequireProjectTokenScope` +
`RequireProjectMember` middleware trio as the rest of the registry surface) and SHALL NOT use a bare
`admin` API-token scope.

#### Scenario: Builtin tool invoked with arguments

- **WHEN** a project member posts arguments to a builtin tool, e.g. `search-semantic`
- **THEN** the tool executes in-process and the response contains the tool result

#### Scenario: External tool invoked with arguments

- **WHEN** a project member posts arguments to a tool on a registered `stdio`/`sse`/`http` server
- **THEN** the call is proxied to that exact server (resolved by server id, not by tool-name prefix)
  and the response contains the tool result

#### Scenario: Empty body invokes with no arguments

- **WHEN** the request body is absent or has no `arguments`
- **THEN** the tool is invoked with nil arguments

#### Scenario: Server is scoped to the caller's project

- **WHEN** the addressed server id belongs to a different project (or does not exist)
- **THEN** the endpoint responds `404` and does not disclose the server's existence

### Requirement: Invocation respects enablement and per-tool authority

The endpoint SHALL refuse to invoke a tool when its server is disabled, when the tool row is
disabled, or when the caller is not authorized for that specific tool. Builtin dispatch SHALL enforce
the same per-tool authority as the MCP HTTP transport — tools marked agent-only SHALL NOT be
directly invocable, and scope- or superadmin-gated tools SHALL be checked against the caller —
rather than blanket-trusting the REST surface.

#### Scenario: Disabled server refuses invocation

- **WHEN** a project member invokes a tool on a disabled server
- **THEN** the endpoint responds `409` (`mcp_server_disabled`) and no call is made

#### Scenario: Disabled tool refuses invocation

- **WHEN** a project member invokes a disabled tool on an enabled server
- **THEN** the endpoint responds `409` (`mcp_tool_disabled`) and no call is made

#### Scenario: Agent-only tool is not directly invocable

- **WHEN** a project member invokes a builtin tool marked agent-only (e.g. `web-fetch`)
- **THEN** the endpoint responds `403` (`mcp_tool_forbidden`) and the tool is not executed

#### Scenario: Scope-gated tool requires the caller's scope

- **WHEN** a project member invokes a builtin tool whose required scope the caller does not hold
- **THEN** the endpoint responds `403` and the tool is not executed

### Requirement: Invocation failures and results have a stable contract

A completed call SHALL respond `200` with the tool result as `data` in the standard success envelope
(`{content: [{type, text}], isError, structuredContent}`); `isError` reports a call-level failure, not
a negative business result. Upstream/connection failures SHALL respond `502` with a sanitized message
that does not expose server connection details (command lines, URLs, secrets); the detail SHALL be
logged server-side. The call SHALL be bounded by a timeout so a stalled server cannot hang the
request, and the response SHALL NOT echo the server's configured env or headers.

#### Scenario: Tool result is returned in the envelope

- **WHEN** a tool call completes
- **THEN** the response is `200` with `data.content` (and `isError` when the tool reported an error)

#### Scenario: Upstream failure is sanitized

- **WHEN** the proxied server cannot be reached or the call times out
- **THEN** the response is `502` with a generic message and the underlying detail is only logged

#### Scenario: Stalled server cannot hang the request

- **WHEN** the proxied server never responds
- **THEN** the call is aborted at the invocation timeout and reported as a `502`
