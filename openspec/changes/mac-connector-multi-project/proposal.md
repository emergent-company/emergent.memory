## Why

A Mac connector can serve **exactly one** Memory project at a time. A developer
who works across several projects (a monorepo backend, a data project, a
personal knowledge project) must pick one project to expose their local Apple
tools (Notes, Reminders) and hosted MCP servers to; connecting a second project
disconnects the first. The single-connection assumption is encoded at every
layer: the engine config (`apps/connector.linux/internal/config/config.go` — one
`token` + `project_id`), the relay client (`internal/relay/client.go`), and on the
Mac side `ProjectStore.connectedProjectID: String?`,
`ProjectProfile.connected` (mutually exclusive), `EngineLifecyclePolicy`, and
`EngineManager`'s single child / single config path / single management port
(`8931`).

We want N projects connected simultaneously, with **no hard cap**.

## What Changes

- **Hub is already ready** — `apps/server/domain/mcprelay/service.go` keys relay
  sessions by `(projectID, instanceID)`, and project identity comes from the
  connection's bearer token, so simultaneous sessions for the same machine across
  different projects already coexist. **No hub or relay-protocol change.**
- **Engine binary is reused unchanged.** The engine already does everything
  per-project correctly (per-config `disabled_tools` / `instance_id` / managed
  config / status).
- **N engine processes** — the Mac app supervises **one engine child per
  connected project**, each bound to its own config file and its own loopback
  management port. Chosen for failure isolation: one project's expired token,
  relay outage, or crash must not take down the others.
- **Mac app model** — `connectedProjectID: String?` → a connected **set**;
  `ProjectProfile` gains a per-project management port and no longer forces
  mutual exclusion; `EngineLifecyclePolicy` and the reconcile loop become
  per-project; the supervisor becomes a map of children.
- **No config-schema change** — multi-project is N single-profile config files
  via the existing `MaterializeWithOptions`; the legacy
  `~/.config/memory-connector.yml` + port `8931` remain the first/primary
  project's defaults (back-compat).
- **UI** — footer/status become "N projects connected"; per-project
  connect/disconnect; hosted-MCP-servers page becomes project-scoped.

## Capabilities

### Modified Capabilities

- `mac-connector-app`: engine supervision, connect/disconnect, reconcile, and
  status generalize from one project to a set of simultaneously-connected
  projects; per-project config binding and management port.
- `mcp-connector`: a machine MAY register with multiple project relay hubs
  simultaneously; re-registration replacement is scoped to the same project.

### New Capabilities

- none.

## Impact

- **Mac app**: `ProjectStore`, `ProjectProfileStore`, `EngineLifecyclePolicy`,
  `EngineManager`, `AppEnvironment` (reconcile), `StatusMonitor`, `AppStatus`,
  `MainWindowView` (footer), `ProjectConnectionControl`,
  `MCPServersStore`/`MCPServersAPIClient` (project-scoped), `ManagementPortProbe`.
- **Engine (Go)**: no behavioural change; possibly a per-project config-path
  helper. Hub: none. Protocol: none.
- **Spec**: `mac-connector-app`, `mcp-connector`.

## Status

Design-first. This change currently contains the proposal, design, and delta
specs only; implementation is tracked in `tasks.md` and will follow on this
branch.
