## MODIFIED Requirements

### Requirement: Embed and supervise the connector engine

The app SHALL bundle the connector engine binary in its resources and run it as a
direct child process (not via launchd or LaunchServices), restarting it with a
circuit breaker if it exits, and stopping it cleanly (SIGTERM) when the app
quits. The engine SHALL run only when the on-disk engine config binds the
**connected** project — and, when the active account's server is known, that
server. A config that exists but binds a different project SHALL be treated as a
configuration error: the engine is not started (or is stopped if running) and a
message naming both project ids is reported. The circuit breaker's window SHALL
reset only for a restart caused by a genuine configuration change, so an
app-driven stop/start oscillation can still trip it. The app SHALL refuse to
spawn when the loopback management port is already held by another process, and
SHALL surface the engine's own management-port bind failure rather than leaving
it only in a log file. Start and stop transitions SHALL be logged only when a
process is actually started or terminated. A restart SHALL sequence on the
previous child process actually exiting before the successor is spawned, so the
successor can bind the loopback management port instead of losing it to its
terminating predecessor; a child that has not exited after SIGTERM within a
bounded grace period SHALL be killed. An explicit stop SHALL cancel any queued
restart.

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

#### Scenario: Config binds a different project

- **WHEN** a project is connected and the engine config on disk binds a
  different project id (or an empty one)
- **THEN** the engine is not started, and a configuration error naming the
  connected and configured project ids is reported

#### Scenario: Config binds a different server

- **WHEN** the config's project matches but its server URL does not match the
  active account's server, and the expected server is known
- **THEN** the engine is not started, and the same configuration error is reported

#### Scenario: Config matches the connected project

- **WHEN** the config's project id matches the connected project, and either no
  expected server was supplied or the server URLs match
- **THEN** the engine runs

#### Scenario: Circuit breaker bounds an oscillation

- **WHEN** the engine exits unexpectedly several times within the breaker window
  without an intervening configuration change
- **THEN** the app stops restarting it and reports that auto-restart stopped

#### Scenario: Management port already held

- **WHEN** the app has no live engine child and something else is already
  listening on the loopback management port
- **THEN** it does not spawn a second engine, and reports that the port is in use

#### Scenario: Engine cannot bind the management port

- **WHEN** the engine's stderr reports the management API could not bind
- **THEN** the app surfaces that failure instead of leaving it only in the log file

#### Scenario: Restart waits for the previous child

- **WHEN** the app restarts a running engine (a configuration-change restart)
- **THEN** it terminates the previous child and does not spawn its successor until that child has exited, so the successor binds the loopback management port

#### Scenario: Hung child is killed

- **WHEN** a child does not exit within the bounded grace period after SIGTERM
- **THEN** the app kills it so the queued restart is not wedged indefinitely

#### Scenario: Queued restart is cancelled by stop

- **WHEN** the user (or sign-out) explicitly stops the engine while a restart is queued
- **THEN** no successor is spawned

#### Scenario: No process stopped

- **WHEN** the app stops an engine that is not running
- **THEN** no stop transition is logged

### Requirement: Manage hosted MCP servers from the app

The app SHALL let the user list, create, edit, enable/disable, and delete the connector's hosted MCP servers through the engine's loopback management API (`http://127.0.0.1:8931`), and SHALL show each server's transport, enabled state, live connection status, discovered tools, and tool count. The app SHALL pass `--api-port 8931` when launching the engine so the API is available, and SHALL NOT send server commands, URLs, environment variables, or headers to Memory. The page SHALL load its data only while the connector engine is running: while it is starting it SHALL show a connecting state, while no project is connected it SHALL show a placeholder directing the user to connect a project, and while the connector is unavailable it SHALL show a generic unavailable message with a retry action. The `Add` and `Browse registry` actions SHALL be disabled while the connector is not running. The page SHALL NOT surface the underlying loopback transport error or other internal engine detail.

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

- **WHEN** the connector engine is not running and the page cannot reach the loopback management API
- **THEN** the page shows a generic unavailable state with a retry action, does not show the raw transport error, and disables the `Add` and `Browse registry` actions

#### Scenario: No project connected

- **WHEN** the page is opened while no project is connected
- **THEN** it shows a placeholder directing the user to connect a project rather than an error

#### Scenario: Connector starting

- **WHEN** the page is opened while the connector engine is starting or restarting
- **THEN** it shows a connecting state and loads the server list once the engine is running

#### Scenario: Secrets stay local and masked

- **WHEN** the app displays a server's environment variables or headers
- **THEN** their values are masked by default with a per-row reveal control, and the app states that these values are stored locally in the connector's config in plaintext and never sent to Memory
