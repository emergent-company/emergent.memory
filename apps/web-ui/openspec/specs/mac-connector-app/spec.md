# mac-connector-app Specification

## Purpose

Lets a Memory user run the connector engine from a small native menu-bar Mac app: sign in with a Memory account, choose a project, choose which local Apple MCP tools are enabled, and connect them to Memory through the embedded engine — with each macOS Automation permission granted once to the app and its state visible in-app.

## Requirements

### Requirement: Embed and supervise the connector engine

The app SHALL bundle the connector engine binary in its resources and run it as a direct child process (not via launchd or LaunchServices), restarting it with a circuit breaker if it exits, and stopping it cleanly (SIGTERM) when the app quits.

#### Scenario: App launch starts the engine

- **WHEN** the app launches with a configured project
- **THEN** the embedded engine starts as a direct child and registers the enabled tools with the Memory hub

#### Scenario: Engine crashes

- **WHEN** the engine process exits unexpectedly
- **THEN** the app restarts it, bounded by a circuit breaker, without user action

#### Scenario: Quitting the app stops the engine

- **WHEN** the user quits the app
- **THEN** the engine child receives SIGTERM and the relay connection closes cleanly

#### Scenario: Engine logs are captured

- **WHEN** the engine writes to stdout/stderr
- **THEN** the app streams those lines to its log file

### Requirement: Require sign-in and connect a project

The app SHALL require connector CLI sign-in before the connector can run and
SHALL NOT accept a manually entered project token. When the signed-in user
connects a project, the connector CLI mints or reuses that project's token and
writes the engine config file (0600); the app persists only non-secret defaults
and per-project profiles.

#### Scenario: Signed out

- **WHEN** no account is signed in
- **THEN** the Connection page shows the sign-in affordance and offers no token entry

#### Scenario: Connect a project

- **WHEN** the signed-in user connects a project
- **THEN** the connector CLI mints or reuses the project token, writes the engine config file, and the engine (re)starts with the new configuration

#### Scenario: Token never written in plaintext config by the app

- **WHEN** the app manages non-secret settings
- **THEN** the project token is owned by the connector CLI, and any engine config file on disk is written by the CLI with restrictive permissions

### Requirement: Signed-out state prompts re-authentication

When the app has an account but the connector CLI has no valid session (an
expired or cleared session), the app SHALL show a signed-out state with a clear
Sign in action instead of a raw "Couldn't load projects" error. Detecting the
failure SHALL prefer the connector CLI's own session status (`auth status`),
falling back to the failure text (`not signed in`, `401`, `invalid_token`). A
successful sign-in SHALL import that session into the connector CLI via
`auth import` and then load the user's projects. Non-auth failures SHALL still
show the error state.

#### Scenario: Auth failure shows the signed-out state

- **WHEN** loading projects fails because the connector CLI reports no valid
  session (or the failure is a `not signed in`, `401`, or `invalid_token`
  error)
- **THEN** the Project & Account page and the project switcher show a
  "Signed out" prompt with a Sign in action (and a Try again action), not a raw
  "Couldn't load projects" error

#### Scenario: Sign-in imports the session and loads projects

- **WHEN** the user signs in from the signed-out prompt
- **THEN** the app imports the session into the connector CLI via
  `auth import` and loads the user's projects for the signed-in environment

#### Scenario: Non-auth failure still shows the error state

- **WHEN** loading projects fails for a reason that is not an auth/session
  failure
- **THEN** the app shows the "Couldn't load projects" error state with the
  failure message, as before

### Requirement: Accounts are listed by email and switching uses the effective signed-in state

The account switcher SHALL list accounts by their EMAIL address (with the
environment as a label), never by initials, avatar, display-name-only text, or a
generic "Signed in" fallback. The control SHALL be driven by the effective
signed-in state — an active account AND a valid connector CLI session — not by
the non-secret account index alone. When the session is invalid the switcher
SHALL show a single Sign in affordance and no account rows; when it is valid the
accounts are listed and switching selects the chosen account (active
checkmarked).

#### Scenario: Signed in lists emails and switching works

- **WHEN** the app has an active account with a valid connector CLI session
- **THEN** the account control lists each account by its email address with its
  environment label, the active account is checkmarked, and choosing another
  account switches to it

#### Scenario: Invalid session shows Sign in with no account rows

- **WHEN** the non-secret index has accounts but the connector CLI reports no
  valid session
- **THEN** the account control shows a single Sign in affordance and does not
  list any account rows

#### Scenario: No generic or initials-only labels

- **WHEN** an account row is rendered
- **THEN** its identity text is the account's email address (falling back to the
  environment label), never the initials or a generic "Signed in"

### Requirement: Choose which local MCP tools run

The app SHALL list the engine's local Apple MCP tools with per-tool enable toggles and persist the disabled set to the engine configuration so registration and serving honor it.

#### Scenario: Toggle a tool off

- **WHEN** the user disables a tool in Settings
- **THEN** the engine re-registers without that tool and rejects calls to it

#### Scenario: Toggle a tool on

- **WHEN** the user enables a previously disabled tool
- **THEN** the engine re-registers with that tool included

### Requirement: Manage hosted MCP servers from the app

The app SHALL let the user list, create, edit, enable/disable, and delete the connector's hosted MCP servers through the engine's loopback management API (`http://127.0.0.1:8890`), and SHALL show each server's transport, enabled state, live connection status, discovered tools, and tool count. The app SHALL pass `--api-port 8890` when launching the engine so the API is available, and SHALL NOT send server commands, URLs, environment variables, or headers to Memory.

#### Scenario: List hosted servers with status

- **WHEN** the user opens the hosted MCP servers page
- **THEN** the app lists each configured server with its transport, enabled state, connection status (colored dot), tool count, and any error, plus an empty state when none exist

#### Scenario: Create a stdio server

- **WHEN** the user adds a server with transport `stdio`, a name, and a command (with optional arguments and environment variables)
- **THEN** the server is created through the management API, persisted to the connector's local config, and appears in the list

#### Scenario: Create an http or sse server

- **WHEN** the user adds a server with transport `http` or `sse`, a name, and a URL (with optional headers)
- **THEN** the server is created through the management API, persisted to the connector's local config, and appears in the list

#### Scenario: Edit a server

- **WHEN** the user edits a server's name, transport, or connection fields and saves
- **THEN** the app replaces the server's config through the management API and shows the updated state

#### Scenario: Enable or disable a server

- **WHEN** the user toggles a server's enabled switch
- **THEN** the app updates only that server's enabled flag through the management API without changing its other fields

#### Scenario: Delete a server with confirmation

- **WHEN** the user chooses to delete a server and confirms the destructive prompt
- **THEN** the server is removed from the connector's local config and hosting of its tools stops

#### Scenario: View discovered tools

- **WHEN** the user opens a server's detail
- **THEN** the app lists the tools discovered on that server by name and description, and reports when it is not connected or has none

#### Scenario: Management API unreachable

- **WHEN** the engine is not running and the app cannot reach the loopback management API
- **THEN** the page shows an error state with a retry action instead of an empty or broken list

#### Scenario: Secrets stay local and masked

- **WHEN** the app displays a server's environment variables or headers
- **THEN** their values are masked by default with a per-row reveal control, and the app states that these values are stored locally in the connector's config in plaintext and never sent to Memory

### Requirement: Choose which hosted MCP server tools are shared

The app SHALL let the user share or unshare each tool a hosted MCP server exposes from that server's detail view, using a per-tool toggle that reflects the server's `disabled_tools` list. Unsharing a tool SHALL add its server-local (un-namespaced) name to `disabled_tools`; sharing it SHALL remove that name. The app SHALL persist the change by fetching the server's full config and replacing it through the management API, preserving every other field (name, transport, command, args, env, url, headers, enabled). The app SHALL disable the sharing toggles while a change is in flight, show a saving indicator, and surface a fetch or save failure without changing the rendered sharing state.

#### Scenario: Toggle a tool off

- **WHEN** the user unshares a tool in a server's detail view
- **THEN** the tool's server-local name is added to the server's `disabled_tools`, the full config is replaced through the management API with all other fields preserved, and the tool shows as "Not shared"

#### Scenario: Toggle a tool back on

- **WHEN** the user shares a tool the server has listed in `disabled_tools`
- **THEN** the tool's name is removed from `disabled_tools` and the full config is replaced with all other fields preserved

#### Scenario: Disabled tools stay visible

- **WHEN** a server has tools listed in `disabled_tools` that its live tools endpoint no longer reports
- **THEN** the detail view still lists those tools as "Not shared" with a toggle to share them again

#### Scenario: Sharing save fails

- **WHEN** fetching the server's config or replacing it fails
- **THEN** the app surfaces the error, leaves the previously rendered sharing state intact, and does not report success

#### Scenario: Sharing save in flight

- **WHEN** a sharing change is being saved
- **THEN** the sharing toggles are disabled and a saving indicator is shown until the save completes

### Requirement: Show connection and tool status

The menu bar SHALL indicate engine/hub state, and the app SHALL show connection state and the currently registered tool list.

#### Scenario: Connected node visible in Memory

- **WHEN** the engine is running and registered with the hub
- **THEN** the app reports connected, the registered tool list, and that the node appears in Memory's `/settings/mcp-nodes`

#### Scenario: Engine stopped or hub unreachable

- **WHEN** the engine is not running or the hub is unreachable
- **THEN** the menu-bar status reflects the disconnected state without a broken UI

### Requirement: Grant and inspect Automation permissions once

The app SHALL expose, per Apple service it serves (Notes, Reminders), a permission card that probes the service to trigger the one-time macOS Automation prompt and reports the resulting state (granted / denied / unknown), with guidance to System Settings → Privacy & Security → Automation.

#### Scenario: First probe prompts once

- **WHEN** the user activates the Notes or Reminders permission probe and the app has not been granted automation for that service
- **THEN** macOS prompts once to allow the app to control that service, and the card reflects the outcome

#### Scenario: Subsequent probes do not re-prompt

- **WHEN** the user probes again after granting
- **THEN** no new prompt appears and the card shows granted

#### Scenario: Denied state is visible

- **WHEN** the user denies the prompt or previously denied it in System Settings
- **THEN** the card shows denied and links to the Automation settings pane

### Requirement: Run as a menu-bar agent app

The app SHALL present a menu-bar item with a status icon and a compact menu (status summary, tool count, open Settings, Quit) and SHALL NOT require a main window for normal operation.

#### Scenario: Menu-bar presence

- **WHEN** the app is running
- **THEN** its menu-bar item shows engine/hub state and opens the compact menu and Settings

### Requirement: Sidebar navigation separating information from settings

The main window SHALL use a sidebar with two sections — information pages and settings pages — and a detail area that shows the selected page; the previously monolithic Settings view is split into those pages. A project switcher SHALL be reachable from the navigation (e.g. the Project & Account page and/or the sidebar).

#### Scenario: Navigate between pages

- **WHEN** the user selects a sidebar item
- **THEN** the detail area shows that page and the selection is remembered while the app runs

#### Scenario: Information and settings are distinct groups

- **WHEN** the sidebar renders
- **THEN** information pages (overview, project/account) and settings pages (tools, hosted MCP servers, permissions, connection, about) appear in separate sections

#### Scenario: Switch project from navigation

- **WHEN** the user chooses a different project in the switcher
- **THEN** the window reflects the newly active project without a restart

### Requirement: Project switcher groups projects by organisation

The project switcher SHALL group projects under their organisation's display name. When a project's organisation name can be resolved (its organisation id matches a name returned by `GET /api/orgs`), the section header SHALL be that organisation name. Projects whose organisation is unknown or unnamed — no organisation id, the org lookup fails, or it has no match — SHALL group under "Other".

#### Scenario: Projects grouped under their organisation

- **WHEN** the switcher lists projects whose organisation ids resolve to names
- **THEN** each project appears under a section headed by its organisation name

#### Scenario: Unknown organisation falls back to Other

- **WHEN** a project's organisation name cannot be resolved (no organisation id, the org lookup fails, or it has no match)
- **THEN** the project groups under "Other" and the switcher still renders

### Requirement: Dashboard shows the project's organisation when known

The Dashboard page SHALL show the active project's name and, when the project's
organisation name can be resolved (its `orgId` matches an entry from
`GET /api/orgs`), that organisation name. When no organisation name can be
resolved — no `orgId`, the org lookup fails, or it has no match — the page SHALL
omit the organisation line entirely and SHALL NOT show a placeholder such as
"Unknown organisation".

#### Scenario: Dashboard shows the resolved organisation

- **WHEN** the dashboard loads for a project whose organisation id matches an entry from `GET /api/orgs`
- **THEN** the page shows the project name and the resolved organisation name

#### Scenario: Dashboard omits an unresolvable organisation

- **WHEN** the dashboard loads but the project has no organisation id, or `GET /api/orgs` fails or has no matching entry
- **THEN** the page shows the project name and omits the organisation line, with no "Unknown organisation" placeholder

### Requirement: Show connected project and user identity

The app SHALL show the connected Memory project and organisation by NAME (not raw identifiers), and the signed-in user identity (display name, email, avatar or initials). Identifiers SHALL be hidden by default and revealed/copyable through a copy control (reveal on hover, copy on click). When no account is signed in, the page SHALL show a clear sign-in prompt instead of an identity.

#### Scenario: Project and user shown

- **WHEN** the app is signed in and the server is reachable
- **THEN** the Project & Account page shows the project name, the organisation name, and the signed-in user's display name, email, and avatar (or initials)

#### Scenario: Signed out

- **WHEN** no account is signed in
- **THEN** the Project & Account page shows a sign-in prompt that leads to the Connection page

#### Scenario: Identifier hidden but copyable

- **WHEN** the user hovers the copy control next to a project or organisation name
- **THEN** the raw identifier is revealed (tooltip/label) and clicking the control copies it to the clipboard

### Requirement: Account control in the window header

The window header SHALL show an account control at the top right: a properly padded "Sign in" button when not effectively signed in, and — when effectively signed in — a control that opens the account switcher (see "Accounts are listed by email and switching uses the effective signed-in state").

#### Scenario: Signed out

- **WHEN** the user is not effectively signed in
- **THEN** the top-right shows a padded, right-aligned "Sign in" control that starts sign-in

#### Scenario: Signed in

- **WHEN** the user is effectively signed in
- **THEN** the top-right control opens the account switcher, listing the signed-in accounts by email with the active account checkmarked
