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
process is actually started or terminated.

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

#### Scenario: No process stopped

- **WHEN** the app stops an engine that is not running
- **THEN** no stop transition is logged

## ADDED Requirements

### Requirement: Reconcile the engine once per account scope change

Changing the active account SHALL reconcile the engine exactly once, and that
reconcile SHALL happen after the connector CLI has rewritten the engine config
for the new scope. While a scope change is in flight, transitions of the
connected-project id SHALL NOT each trigger their own reconcile. A config
belonging to the previous scope SHALL never be used to start the engine.
Overlapping scope changes SHALL settle in the order they began, each waiting for
the previous one to settle, so that the last change's config rewrite and
reconcile are the ones that stand. The project reload that accompanies an
account change SHALL NOT reconcile the engine a second time; the scope swap's
own settle is the single reconcile.

#### Scenario: Switching accounts reconciles once

- **WHEN** the active account changes and the new scope's config has been written
- **THEN** the engine is reconciled once — not once per connected-project-id transition

#### Scenario: Stale config is never started

- **WHEN** the connected-project id changes during a scope swap, before the new
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

### Requirement: Engine lifecycle is observable in the log

The app SHALL log the account-scope and engine-lifecycle transitions needed to
tell an autonomous reconcile loop apart from user-driven sign-in, account switch,
and sign-out. Each account change SHALL be logged with its source, and each scope
apply with the account, its environment, and the previously connected project.
These lines SHALL be distinguishable from the engine's own output.

#### Scenario: Account change is attributable

- **WHEN** the active account changes (sign-in, switch, or sign-out)
- **THEN** the log records the change with its source and the resulting account

#### Scenario: Scope apply is attributable

- **WHEN** a scope is applied for an account
- **THEN** the log records the account, its environment, and the previously
  connected project id

#### Scenario: Autonomous churn is distinguishable

- **WHEN** scope applies appear in the log without a preceding account-change line
- **THEN** the reconcile churn can be identified as not user-driven
