## MODIFIED Requirements

### Requirement: Embed and supervise the connector engine

The app SHALL bundle the connector engine binary in its resources and run it as
direct child process(es) — not via launchd or LaunchServices — **one child per
connected project**, restarting each with a circuit breaker if it exits, and
stopping them all cleanly (SIGTERM) when the app quits. Each engine child SHALL
run only when its on-disk engine config binds that child's **connected** project
— and, when the active account's server is known, that server. A config that
exists but binds a different project SHALL be treated as a configuration error
**for that project only**: its engine is not started (or is stopped if running)
and a message naming both project ids is reported, while every other connected
project keeps running. The circuit breaker's window SHALL reset only for a
restart caused by a genuine configuration change, so an app-driven stop/start
oscillation can still trip it. The app SHALL refuse to spawn a project's child
when that child's loopback management port is already held by another process,
and SHALL surface the engine's own management-port bind failure rather than
leaving it only in a log file. Start and stop transitions SHALL be logged only
when a process is actually started or terminated.

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

- **WHEN** a connected project's engine config on disk binds a different project
  id (or an empty one)
- **THEN** that project's engine is not started, a configuration error naming
  that project and the configured project id is reported, and other connected
  projects keep running

#### Scenario: Config binds a different server

- **WHEN** a config's project matches its project but its server URL does not
  match the active account's server, and the expected server is known
- **THEN** that project's engine is not started, and the same configuration error is reported

#### Scenario: Config matches the connected project

- **WHEN** a config's project id matches its connected project, and either no
  expected server was supplied or the server URLs match
- **THEN** that project's engine runs

#### Scenario: Circuit breaker bounds an oscillation

- **WHEN** an engine exits unexpectedly several times within the breaker window
  without an intervening configuration change
- **THEN** the app stops restarting that engine and reports that auto-restart stopped

#### Scenario: Management port already held

- **WHEN** the app has no live engine child for a project and something else is
  already listening on that project's loopback management port
- **THEN** it does not spawn a second engine for that project, and reports that
  the port is in use

#### Scenario: Engine cannot bind the management port

- **WHEN** the engine's stderr reports the management API could not bind
- **THEN** the app surfaces that failure instead of leaving it only in the log file

#### Scenario: No process stopped

- **WHEN** the app stops an engine that is not running
- **THEN** no stop transition is logged

### Requirement: Require sign-in and connect a project

The app SHALL require connector CLI sign-in before the connector can run and
SHALL NOT accept a manually entered project token. The signed-in user MAY connect
**any number** of projects simultaneously. When the user connects a project, the
connector CLI mints or reuses that project's token and writes that project's
engine config file (0600); the app persists only non-secret defaults and
per-project profiles. Connecting a project SHALL NOT disconnect any other
connected project; disconnecting is per project.

#### Scenario: Signed out

- **WHEN** no account is signed in
- **THEN** the Connection page shows a single prominent sign-in action for the
  Production environment and offers no token entry and no environment choice

#### Scenario: Connect a project

- **WHEN** the signed-in user connects a project
- **THEN** the connector CLI mints or reuses the project token, writes the engine config file, and the engine (re)starts with the new configuration

#### Scenario: Connect a second project keeps the first connected

- **WHEN** the signed-in user connects a second project while a first is connected
- **THEN** both projects remain connected, each with its own engine child, config
  file, token, and management port

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
show the error state. When one or more projects are connected and a re-auth
failure affects one project, the app SHALL keep the **per-project** connected
intent for the projects whose configs still bind, and SHALL NOT stop a running
engine child whose config still binds its project; only the failing project's
management actions require re-authentication, and no other project's engine child
is affected.

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

#### Scenario: Session expires while a project is connected

- **WHEN** one or more projects are connected and the connector CLI's session
  expires or is cleared, so a config re-materialisation (e.g. `projects use`
  during the launch scope-settle) fails with an auth/session error
- **THEN** the app keeps the per-project connected intent for projects whose
  configs still bind and leaves their running engines running (their long-lived
  project tokens and on-disk configs still bind those projects), and surfaces the
  signed-out prompt for management actions — it does not tear down healthy engines

### Requirement: Show connection and tool status

The menu bar SHALL indicate engine/hub state, and the app SHALL show connection
state — as the **number of currently connected projects** — and the currently
registered tool list.

#### Scenario: Connected node visible in Memory

- **WHEN** the engine is running and registered with the hub
- **THEN** the app reports connected, the registered tool list, and that the node appears in Memory's `/settings/mcp-nodes`

#### Scenario: Engine stopped or hub unreachable

- **WHEN** the engine is not running or the hub is unreachable
- **THEN** the menu-bar status reflects the disconnected state without a broken UI

### Requirement: Reconcile the engine once per account scope change

Changing the active account SHALL reconcile the engine set exactly once, and that
reconcile SHALL happen after the connector CLI has rewritten the engine configs
for the new scope. While a scope change is in flight, transitions of the
connected-project set SHALL NOT each trigger their own reconcile. A config
belonging to the previous scope SHALL never be used to start an engine. All
engine children of the previous scope SHALL be stopped before the new scope's set
is reconciled. Overlapping scope changes SHALL settle in the order they began,
each waiting for the previous one to settle, so that the last change's config
rewrites and reconcile are the ones that stand. The project reload that
accompanies an account change SHALL NOT reconcile the engine a second time; the
scope swap's own settle is the single reconcile.

#### Scenario: Switching accounts reconciles once

- **WHEN** the active account changes and the new scope's config has been written
- **THEN** the engine is reconciled once — not once per connected-project-id transition

#### Scenario: Stale config is never started

- **WHEN** the connected-project set changes during a scope swap, before the new
  config has been written
- **THEN** the engine is not started against the previous scope's config

#### Scenario: Overlapping scope changes stay suppressed

- **WHEN** a second account change begins before the first has settled
- **THEN** connected-project-id transitions remain suppressed until the last
  scope change settles, and the final reconcile reflects the settled state

#### Scenario: Overlapping scope changes settle in order

- **WHEN** two account changes overlap
- **THEN** the second settles after the first, and the last change's config
  rewrite and reconcile are the ones that stand

#### Scenario: The account-change reload does not double-reconcile

- **WHEN** the active account changes
- **THEN** the engine is reconciled by the scope swap's settle alone, and the
  accompanying project reload does not reconcile it again

#### Scenario: Direct connect and disconnect still reconcile

- **WHEN** the user connects or disconnects a project with no account change in flight
- **THEN** the engine is reconciled as before

## ADDED Requirements

### Requirement: Multiple project connections run concurrently and independently

The app SHALL allow multiple Memory projects to be connected at the same time,
with no fixed cap, on the same Mac. Each connected project SHALL have its own
engine process, its own engine config file, its own project-scoped token, and its
own loopback management port. A failure affecting one project — an expired
session, an unreachable hub, a configuration mismatch, an engine crash, or a
management-port conflict — SHALL NOT stop or degrade any other connected
project's engine. Connected projects SHALL be individually connectable and
disconnectable. The first-connected project SHALL keep the legacy config path and
the default management port so a single-project user sees no behavioural change.

#### Scenario: Two projects connected at once

- **WHEN** the user connects two projects
- **THEN** the app runs two engine children, each bound to its own project's
  config and management port, and both register with the hub under their own
  project sessions

#### Scenario: One project's failure does not affect the others

- **WHEN** one connected project's engine crashes, its hub becomes unreachable,
  or its management port is held
- **THEN** only that project is affected (its child restarts, stops, or reports
  the error) and every other connected project's engine keeps running

#### Scenario: Disconnect one project only

- **WHEN** the user disconnects one of several connected projects
- **THEN** only that project's engine child stops; the others remain connected

#### Scenario: Per-project management ports do not collide

- **WHEN** a new project is connected while other projects already have engine
  children with management ports
- **THEN** a free port is selected and persisted for the new project, and no two
  project children share a port

#### Scenario: Registry browse targets the active project

- **WHEN** the user opens the MCP registry browse sheet while a non-primary
  project is the active connected project
- **THEN** registry search and detail requests go to that project's management
  port, not the default `8931`, and re-target if the active project changes
