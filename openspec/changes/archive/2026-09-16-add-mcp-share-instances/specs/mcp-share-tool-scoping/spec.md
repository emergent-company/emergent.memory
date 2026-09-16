## Purpose

Restricts an MCP share instance to an explicit allowlist of memory tools, enforced both when a client lists tools and when it calls one, so an outside agent cannot see or invoke anything the project did not grant.

## ADDED Requirements

### Requirement: Tool listing is limited to the instance allowlist

When a request is authenticated with an MCP share instance token that has a tool allowlist, `tools/list` SHALL return only tools that are both in the instance allowlist and permitted by the token's scopes. When an instance has an unrestricted (null) allowlist, `tools/list` SHALL return all tools permitted by the token's scopes.

#### Scenario: Allowlisted tool is listed

- **WHEN** an instance is allowed tool `entity-search` and its token has the required scope
- **THEN** `tools/list` includes `entity-search`

#### Scenario: Non-allowlisted tool is hidden

- **WHEN** an instance is allowed only `entity-search` but the token's scopes also permit `schema-list`
- **THEN** `tools/list` omits `schema-list`

#### Scenario: Scope still applies on top of the allowlist

- **WHEN** an instance's allowlist includes a tool whose required scope the token does not hold
- **THEN** `tools/list` omits that tool

#### Scenario: Unrestricted allowlist lists scope-permitted tools

- **WHEN** an instance has a null allowlist
- **THEN** `tools/list` returns every tool permitted by the token's scopes

### Requirement: Tool calls are rejected outside the instance allowlist

A `tools/call` request authenticated with an instance token SHALL be rejected, before any side effect, when the requested tool is not in the instance allowlist. The client MUST receive a structured JSON-RPC error and the tool MUST NOT execute.

#### Scenario: Allowlisted tool call succeeds

- **WHEN** a client calls a tool that is in the instance allowlist and permitted by scope
- **THEN** the tool executes and returns its result

#### Scenario: Non-allowlisted tool call is rejected

- **WHEN** a client calls a tool that is not in the instance allowlist but is otherwise permitted by scope
- **THEN** the call is rejected with a structured error and the tool does not execute

#### Scenario: Rejection occurs before side effects

- **WHEN** a client calls a non-allowlisted mutating tool
- **THEN** no data is created, modified, or deleted

### Requirement: Tool allowlist entries are validated on write

A tool allowlist supplied when creating or updating an instance SHALL be validated against the includable memory tool catalog. Unknown tool names MUST be rejected, and the allowlist MUST be de-duplicated.

#### Scenario: Unknown tool name rejected

- **WHEN** a create or update request includes a tool name that is not an includable memory tool
- **THEN** the request is rejected with an unprocessable-entity error naming the unknown tool and the instance is unchanged

#### Scenario: Duplicate entries collapsed

- **WHEN** a request supplies the same tool name twice
- **THEN** the stored allowlist contains that tool exactly once

### Requirement: Instance token scopes follow the allowlist

The scopes granted to an instance's token SHALL be derived from the required scopes of the tools in its allowlist, plus `projects:read`, and MUST NOT include scopes unrelated to the allowlist. When the allowlist changes, the token's scopes SHALL be updated to match.

#### Scenario: Scopes derived from selected tools

- **WHEN** an instance allows only read-oriented tools
- **THEN** its token carries only the scopes those tools require, plus `projects:read`

#### Scenario: Scopes shrink when tools are removed

- **WHEN** an admin removes the only tool requiring a given scope from an instance
- **THEN** the token no longer carries that scope

#### Scenario: Explicit tools do not grant write scopes

- **WHEN** an instance allows only tools whose required scope is read-only
- **THEN** its token carries no write scope

### Requirement: Agent-only tools are never exposed to share instances

Tools marked agent-only SHALL NOT be included in an instance's allowlist or returned by `tools/list`, regardless of the instance configuration.

#### Scenario: Agent-only tool excluded

- **WHEN** an instance's configuration references an agent-only tool
- **THEN** the request is rejected on write or the tool is omitted from `tools/list`

### Requirement: Agent execution cannot bypass the instance tool allowlist

The instance tool allowlist SHALL be a real execution boundary, not merely a listing/transport filter. The system MUST enforce the allowlist inside tool execution itself, so that tools invoked transitively during a triggered agent run are also rejected when they are not allowlisted. A tool allowlist that includes an agent-execution or agent-mutation tool — whose side effects run outside the instance request context — MUST be rejected on create/update, and such tools MUST NOT be offered by the tool catalog.

#### Scenario: Triggered agent cannot call a non-allowlisted tool

- **WHEN** an instance has a tool allowlist and a triggered agent attempts to call a tool outside that allowlist
- **THEN** the call is rejected before the tool executes, even though it did not pass through the transport allowlist check

#### Scenario: Agent-execution tools cannot be allowlisted

- **WHEN** a create or update request includes an agent-execution or agent-mutation tool in `tools`
- **THEN** the request is rejected with an unprocessable-entity error

#### Scenario: Catalog excludes agent-execution tools

- **WHEN** the tool catalog is requested
- **THEN** agent-execution and agent-mutation tools are omitted
