## ADDED Requirements

### Requirement: Builtin server renders as "Memory tools" with grouped tools

The MCP Servers page SHALL render memory's builtin server as **"Memory tools"** rather than its raw
registry name `builtin`, and SHALL NOT render a second badge repeating `builtin`. The builtin row's
tools SHALL be presented in the builtin capability groups returned by
`GET /api/admin/builtin-tool-groups` — each a collapsible group with its label, tool count, optional
description, and its member tools (with descriptions) — matching the visual language of the agent
tool picker. Tools not covered by any group SHALL remain reachable in an "Other" group. The gateway
SHALL render the server-owned grouping and SHALL NOT derive the taxonomy locally. The builtin row
SHALL remain read-only for configuration (no sync/inspect/edit/delete, no per-tool toggles).

#### Scenario: Builtin row identity

- **WHEN** a user opens the MCP Servers page
- **THEN** the builtin server row reads "Memory tools" and does not repeat the `builtin` label

#### Scenario: Builtin tools are grouped

- **WHEN** the builtin row is expanded
- **THEN** its tools are listed in collapsible capability groups, with tools not covered by a group
  shown in an "Other" group

#### Scenario: Grouping fetch fails

- **WHEN** the builtin tool-groups endpoint is unavailable
- **THEN** the page still renders and the builtin tools fall back to the flat cached-tool list

### Requirement: Any tool can be run with caller-supplied arguments from the registry

Each tool row on the MCP Servers page SHALL offer a "Run" affordance that opens a dialog accepting a
JSON object of arguments (empty = no arguments). Submitting SHALL invoke the tool through
`POST /api/mcp-servers/:id/tools/:toolName/call` and render the result in the dialog — the tool's
content text (JSON pretty-printed when applicable), a clear state when the tool reported an error,
and a clear state when the call failed (including authorization, disabled, and upstream failures).
Invalid argument JSON SHALL be rejected inline before any request is made.

The rendered result SHALL be built with DOM nodes/`textContent`; tool output SHALL NEVER be injected
as `innerHTML`.

#### Scenario: Run a tool with arguments

- **WHEN** a user opens the run dialog for a tool, enters valid JSON arguments, and runs it
- **THEN** the tool is invoked and its result is shown in the dialog

#### Scenario: Run a tool with no arguments

- **WHEN** a user runs a tool leaving the arguments field empty
- **THEN** the tool is invoked with no arguments

#### Scenario: Invalid argument JSON

- **WHEN** a user submits arguments that are not a JSON object
- **THEN** the dialog shows an inline error and no request is sent

#### Scenario: Failed call is surfaced

- **WHEN** the call fails (forbidden, disabled, or upstream error)
- **THEN** the dialog shows a clear failure state with the returned message and no raw connection
  details

#### Scenario: Tool output is not injected as HTML

- **WHEN** a tool result is rendered
- **THEN** its text is inserted via DOM text nodes, never `innerHTML`
