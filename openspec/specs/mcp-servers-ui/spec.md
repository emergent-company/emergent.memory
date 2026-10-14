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

### Requirement: Reach MCP Sharing from the MCP Servers page

The MCP Servers page SHALL provide a navigation entry point to the MCP Sharing area so admins can move between managing servers the project consumes and shares the project exposes.

#### Scenario: Entry point present

- **WHEN** a user opens the MCP Servers page
- **THEN** a visible link to the MCP Sharing page is shown

#### Scenario: Navigate to sharing

- **WHEN** the user selects the MCP Sharing entry point
- **THEN** the MCP Sharing page opens in the same project context

### Requirement: Registry listing offers a device install for package-only entries

For each registry entry with no remote transport the listing SHALL offer an
"Install to device" action that opens a dialog rather than posting an install
directly. The dialog SHALL let the user pick a connected connector device and
submit the install to that device's instance id. When no device is connected the
dialog SHALL instead show how to connect one — signing in with
`memory-connector auth login` and starting the relay with
`--accept-managed-config` — and SHALL NOT offer a target picker. When the
connected-device list cannot be loaded the dialog SHALL report the failure and
SHALL NOT claim that no device is connected. An entry with a remote transport
SHALL keep its existing outbound install action and SHALL NOT offer a device
install.

#### Scenario: Package-only entry offers a device install

- **WHEN** the registry listing renders an entry with no remote transport
- **THEN** the entry shows an "Install to device" action whose dialog posts to the device install route

#### Scenario: Dialog picks a connected device

- **WHEN** the device install dialog renders while a connector device is connected
- **THEN** it offers each connected device as a selectable install target

#### Scenario: Dialog explains connector setup when no device is connected

- **WHEN** the device install dialog renders with no connected device
- **THEN** it shows the connector sign-in and relay commands and offers no target picker

#### Scenario: Failed device list is reported, not mistaken for no devices

- **WHEN** the connected-device list cannot be loaded
- **THEN** the dialog reports the failure and does not show the connector setup steps

#### Scenario: Remote entry keeps its outbound install

- **WHEN** the registry listing renders an entry with a remote transport
- **THEN** the entry shows the outbound install action and no device install action

### Requirement: Device install reports its outcome in the same way as an outbound install

A device install SHALL follow the same post/redirect/get pattern as the outbound
install. An install that still requires configuration SHALL redirect to that
server's configure form; an install with no required configuration SHALL redirect
to the connected devices page, which reports the install; and a rejected install
SHALL redirect back to the registry listing with an error and SHALL NOT report
success. A submission missing the registry name or the target device SHALL be
rejected by the gateway without calling the backend.

#### Scenario: Device install needing credentials goes to the configure form

- **WHEN** a device install creates a server with required configuration
- **THEN** the response redirects to that server's configure form

#### Scenario: Successful device install confirms on the devices page

- **WHEN** a device install creates a server with no required configuration
- **THEN** the response redirects to the connected devices page with an install confirmation

#### Scenario: Rejected device install reports on the registry listing

- **WHEN** the backend rejects a device install
- **THEN** the response redirects to the registry listing with an error and no install is reported

#### Scenario: Missing target device is rejected by the gateway

- **WHEN** a device install submission carries no target instance id
- **THEN** the gateway redirects with an error without calling the backend

### Requirement: Configure banner explains each missing key and links to docs

When the edit form is opened for configuration, the banner naming the missing required
keys SHALL also show each key's registry description and SHALL link to the provider's
credential documentation derived from the server's registry repository URL. When the
server has no repository URL, no docs link SHALL render.

#### Scenario: Missing key shows its registry description

- **WHEN** the configure banner renders a missing key whose registry declaration includes a description
- **THEN** the banner shows that description alongside the key name

#### Scenario: Provider docs link is shown for a GitHub install

- **WHEN** the configure banner renders a server whose registry repository is a GitHub repository
- **THEN** it links to the GitHub token settings page

#### Scenario: No provenance yields no link

- **WHEN** the configure banner renders a manually-created server with no registry repository URL
- **THEN** no credential docs link is rendered

### Requirement: Authorization headers are entered as a scheme and token

For a required header named `Authorization`, the create/edit form SHALL render a scheme
picker and a token field instead of a free-form header value. The scheme SHALL default
to `Bearer` (or the scheme named by the registry value template when present) and SHALL
be overridable. On submit the form SHALL assemble the header value as
`"<scheme> <token>"`, SHALL NOT double-prefix a token that already carries the scheme,
and SHALL pass the token through unchanged when the raw scheme is selected. A blank
token SHALL leave the stored secret unchanged on edit.

#### Scenario: Token is assembled with the selected scheme

- **WHEN** a user selects `Bearer` and enters `ghp_x`
- **THEN** the stored `Authorization` header value is `Bearer ghp_x`

#### Scenario: Pasting a full value does not double-prefix

- **WHEN** a user pastes `Bearer ghp_x` into the token field with `Bearer` selected
- **THEN** the stored value is `Bearer ghp_x`, not `Bearer Bearer ghp_x`

#### Scenario: Blank token keeps the stored secret

- **WHEN** an edit is submitted with the token field left blank
- **THEN** the previously stored secret is kept

### Requirement: Authentication rejection is reported as actionable

When a remote MCP server answers a connect, inspect, or discovery request with HTTP
401 or 403, memory SHALL report the failure as a credential rejection naming the status
and the expected form of the credential (for an `Authorization` header, `Bearer
<token>`), rather than a generic connection error.

#### Scenario: Test connection reports a rejected credential

- **WHEN** a save-less connection test reaches a server that answers 401
- **THEN** the reported error attributes the failure to the credential and mentions the `Bearer` form

#### Scenario: Discovery reports a rejected credential

- **WHEN** tool discovery reaches a server that answers 403
- **THEN** the surfaced error attributes the failure to the credential rather than reporting an opaque connection error

### Requirement: Device-targeted servers render as device rows

The gateway SHALL render a device-targeted MCP server (`target = 'device'`)
distinctly from an outbound server on the MCP Servers list: the row SHALL show
a `device` badge and, when the server is bound to a specific connector, its
target instance id. The row SHALL NOT offer the host-only outbound affordances
(sync, inspect, or OAuth connect), because the shared host never dials a
device-targeted server. Instead the expanded row SHALL explain that the server
runs on a connector device, and the "no tools cached" hint SHALL say tools are
discovered on the device rather than directing the user to run a host sync.

#### Scenario: Device row shows its target

- **WHEN** the MCP Servers list renders a device-targeted server
- **THEN** the row shows a `device` badge rather than rendering as an ordinary outbound server

#### Scenario: Bound device shows its instance id

- **WHEN** the MCP Servers list renders a device-targeted server bound to a specific connector instance
- **THEN** the row shows that target instance id alongside the `device` badge

#### Scenario: Broadcast device shows no instance id

- **WHEN** the MCP Servers list renders a device-targeted server with no bound instance (broadcast)
- **THEN** the row shows the `device` badge and no instance id

#### Scenario: Device row drops outbound affordances

- **WHEN** the MCP Servers list renders a device-targeted server
- **THEN** the row offers no sync, inspect, or connect actions, while an outbound server's row still does

#### Scenario: Device row explains where it runs

- **WHEN** the user expands a device-targeted server's row
- **THEN** the row explains that the server runs on a connector device, and the empty tools hint does not direct the user to run a host sync
