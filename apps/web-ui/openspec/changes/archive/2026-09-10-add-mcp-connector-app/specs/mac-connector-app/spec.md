## Purpose

Lets a Memory user run the connector engine from a small native menu-bar Mac app: configure a Memory project (server + Keychain-stored token), choose which local Apple MCP tools are enabled, and connect them to Memory through the embedded engine — with each macOS Automation permission granted once to the app and its state visible in-app.

## ADDED Requirements

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

### Requirement: Configure the Memory project

The app SHALL let the user set server URL, project API token, and instance id; store the token in the macOS Keychain; and write the engine config file (0600) from those values before starting the engine.

#### Scenario: Save a valid configuration

- **WHEN** the user enters a reachable server URL, a token, and an instance id and saves
- **THEN** the token is stored in the Keychain, the engine config file is written, and the engine (re)starts with the new configuration

#### Scenario: Token never written in plaintext config

- **WHEN** the app manages configuration
- **THEN** the token's source of truth is the Keychain; any engine config file on disk is materialized from it at runtime with restrictive permissions

#### Scenario: Editing configuration restarts the engine

- **WHEN** the user changes server, token, or instance id and saves
- **THEN** the running engine is stopped and restarted with the updated config

### Requirement: Choose which local MCP tools run

The app SHALL list the engine's local Apple MCP tools with per-tool enable toggles and persist the disabled set to the engine configuration so registration and serving honor it.

#### Scenario: Toggle a tool off

- **WHEN** the user disables a tool in Settings
- **THEN** the engine re-registers without that tool and rejects calls to it

#### Scenario: Toggle a tool on

- **WHEN** the user enables a previously disabled tool
- **THEN** the engine re-registers with that tool included

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

The main window SHALL use a sidebar with two sections — information pages and settings pages — and a detail area that shows the selected page; the previously monolithic Settings view is split into those pages.

#### Scenario: Navigate between pages

- **WHEN** the user selects a sidebar item
- **THEN** the detail area shows that page and the selection is remembered while the app runs

#### Scenario: Information and settings are distinct groups

- **WHEN** the sidebar renders
- **THEN** information pages (overview, project/account) and settings pages (tools, permissions, connection, about) appear in separate sections

### Requirement: Show connected project and user identity

The app SHALL show the connected Memory project and organisation by NAME (not raw identifiers), and the user identity (display name, email, avatar or initials); identifiers SHALL be hidden by default and revealed/copyable through a copy control (reveal on hover, copy on click). The app SHALL degrade gracefully when identity is unavailable (e.g. sandbox token or unreachable server).

#### Scenario: Project and user shown

- **WHEN** the app is configured and the server is reachable
- **THEN** the Project & Account page shows the project name, the organisation name, and the user's display name, email, and avatar (or initials)

#### Scenario: Identifier hidden but copyable

- **WHEN** the user hovers the copy control next to a project or organisation name
- **THEN** the raw identifier is revealed (tooltip/label) and clicking the control copies it to the clipboard

#### Scenario: Identity unavailable

- **WHEN** the token has no user identity or the server is unreachable
- **THEN** the page shows a clear unavailable state and the rest of the app keeps working

### Requirement: Remove a stored fallback token

The app SHALL let the user remove a manually stored API token (the Connection → advanced fallback), returning to the token-less state and stopping the engine.

#### Scenario: Remove fallback token

- **WHEN** the user chooses to remove the stored API token
- **THEN** the token is deleted from the Keychain, the app reports no token configured, and the engine stops

### Requirement: Account control in the window header

The window header SHALL show an account control at the top right: a properly padded "Sign in" button when signed out, and — when signed in — the user's avatar, which opens an account menu with the profile action.

#### Scenario: Signed out

- **WHEN** the user is signed out
- **THEN** the top-right shows a padded, right-aligned "Sign in" control that starts sign-in

#### Scenario: Signed in

- **WHEN** the user is signed in
- **THEN** the top-right shows their avatar (initials), and clicking it opens a menu with the account details and an action that navigates to the profile page


