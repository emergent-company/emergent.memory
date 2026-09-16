# mcp-servers-ui Specification

## Purpose
Lets project owners register, connect, and maintain the MCP servers that power their agents' tools — list/register/edit/delete servers, discover and toggle their tools, all from the gateway UI without direct API access.

## Requirements

### Requirement: MCP servers are listed with connection and tool state
The gateway SHALL render a project-scoped MCP Servers page listing every registered server (builtin and external) with its name, transport type, enabled state, and cached tool count. The page SHALL be reachable from the global sidebar Settings group and SHALL follow the same page-title/test-id conventions as other pages. A project with no external servers SHALL show an empty state with a call-to-action to register one.

#### Scenario: Page renders registered servers
- **WHEN** a user opens the MCP Servers page
- **THEN** a table lists all registered MCP servers with name, type, enabled state, and tool count, and each row offers edit and delete actions

#### Scenario: Empty project
- **WHEN** the project has no registered MCP servers
- **THEN** the page shows an empty state with a "register a server" call-to-action that opens the create form

### Requirement: User can register an external MCP server
The gateway SHALL provide a create form for external servers with fields for a unique name, a transport choice (`stdio` | `sse` | `http`), and the transport-specific connection fields (URL for `sse`/`http`; command/args/env for `stdio`; optional HTTP headers as key/value rows for `sse`/`http`). Submission SHALL validate required fields and reject invalid combinations (e.g. missing URL for `http`, duplicate name, or empty name) with inline errors and no partial save. A successful create SHALL persist the server via the memory admin API and return the user to the list with a success toast; builtin servers SHALL NOT be creatable, editable, or deletable through the UI.

#### Scenario: Register an http server with headers
- **WHEN** a user fills the create form with name, transport `http`, a URL, and one Authorization header and submits
- **THEN** the server is created in the project registry, the list shows it with type `http`, and a success toast confirms creation

#### Scenario: Duplicate name rejected
- **WHEN** a user submits a create form whose name matches an existing server
- **THEN** the form shows an inline error naming the conflict and no server is created

### Requirement: User can edit and delete external servers
The gateway SHALL provide an edit form pre-filled with the server's current connection configuration and SHALL persist changes (including changing transport and enabling/disabling the server). Deleting a server SHALL require confirmation in a dialog, SHALL remove the server and its cached tools from the project, and SHALL immediately strip that server's tools from any agent that referenced them. Builtin servers SHALL NOT be editable or deletable.

#### Scenario: Edit a server's URL
- **WHEN** a user changes a server's URL on the edit form and saves
- **THEN** the updated URL is persisted and shown on the list, and a success toast confirms the update

#### Scenario: Delete with confirmation
- **WHEN** a user clicks delete on a server and confirms in the dialog
- **THEN** the server disappears from the list and agents no longer expose its tools

### Requirement: User can sync and inspect a server's tools
The gateway SHALL expose, per server, a sync action that calls the memory discovery endpoint (`POST /:id/sync`), updates the cached tool list, and surfaces the outcome; when sync removes tools that disappeared from the server, the user SHALL be told which tools were pruned. The gateway SHALL also expose an inspect action (`POST /:id/inspect`) that runs an ephemeral capability probe and shows its result (reachable tools/prompts/resources, or a connection error). Failed sync/inspect against an unreachable server SHALL NOT corrupt the existing cached tools and SHALL surface a clear error message.

#### Scenario: Sync discovers tools
- **WHEN** a user clicks sync on a reachable registered server
- **THEN** the server's cached tool list is refreshed from `tools/list`, newly discovered tools appear with per-tool state, and the tool count on the list updates

#### Scenario: Inspect an unreachable server
- **WHEN** a user clicks inspect on a server whose endpoint is unreachable
- **THEN** the UI shows a connection error and the server's previously cached tools remain intact

### Requirement: User can toggle individual tools
The gateway SHALL render each cached tool of an external server with its current enabled state and SHALL let the user enable/disable it, persisted per tool via the memory admin API. Disabled tools SHALL NOT be offered to agents.

#### Scenario: Disable a tool
- **WHEN** a user disables a tool on a server with cached tools
- **THEN** the toggle state is persisted and the tool no longer appears as available for agent attachment

### Requirement: User can reach the management page from the agent tool picker
When an agent has no tools and no MCP servers are registered, the agent tool picker's empty-state description SHALL link to the MCP Servers management page.

#### Scenario: Agent with no tools navigates to registry
- **WHEN** a user views an agent whose tool picker shows the "register an MCP server" empty state
- **THEN** the empty state links to the MCP Servers page, which opens in the same project context

### Requirement: Secret env and header values are write-only
Each env/header key/value row on the create and edit forms SHALL offer a Plain/Secret selector that always submits a value, so rows stay aligned. When a row is marked Secret, the gateway SHALL send the value to the memory API marked as secret; the memory API SHALL encrypt it at rest and SHALL NEVER return the value in any read (list/get/inspect) response — only the key name SHALL be exposed. The edit form SHALL render secret rows with an empty value and a hint that leaving it blank keeps the stored value; saving an edit or toggling the server enabled/disabled with a blank secret value SHALL preserve the stored secret, while entering a new value SHALL replace it.

#### Scenario: Mark an env value secret
- **WHEN** a user marks an env row Secret, enters a value, and saves
- **THEN** the value is stored encrypted, the server config is saved, and neither the list nor the edit form ever displays the value (the edit form shows a blank secret row)

#### Scenario: Keep an existing secret on edit
- **WHEN** a user edits a server that has a stored secret and leaves that secret row blank
- **THEN** the stored secret is preserved unchanged and the server still authenticates with it

#### Scenario: Toggle preserves secrets
- **WHEN** a user enables or disables a server that has stored secret env/header values
- **THEN** the toggle persists without clearing or overwriting the stored secrets
