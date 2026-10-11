# Design: Multiple simultaneous project connections

## Context

The connector currently serves one project at a time. This document records the
architecture decision for allowing **N projects to stay connected at once**.
Recon was done on the current default branch.

## Current model (where "one project" is encoded)

- **Engine config** — `apps/connector.linux/internal/config/config.go:29-43`: a
  single profile (`ServerURL`, `Token`, `ProjectID`, `InstanceID`, `DisabledTools`,
  `Tools`, `MCPServers`, …). `Validate()` requires one token; `Materialize`/
  `MaterializeWithOptions` write one file for one `(serverURL, token, projectID)`.
- **Relay client** — `internal/relay/client.go:82-121`: one `Client` (one token,
  one `projectID`, one connection, one reconnect loop).
- **Hub** — `apps/server/domain/mcprelay/service.go:290-349`: sessions keyed
  `projectID + "/" + instanceID`. Project identity comes from the bearer token
  (`domain/mcprelay/handler.go:50`, `pkg/auth/middleware.go:76-93`).
- **Mac app** — `ProjectStore.swift:90` (`connectedProjectID: String?`),
  `ProjectProfileStore.swift:7-37,106-108,141-157` (`connected: Bool`, mutually
  exclusive), `EngineLifecyclePolicy.swift:15-86` (single decision),
  `EngineManager.swift:33,143` (one child, one config path),
  `ManagementPortProbe.swift:17` (single port `8931`),
  `EngineConfigWriter.swift`/`EngineConfigSync.swift` (one config),
  `AppEnvironment.swift:292-335` (`syncEngineWithConnection`).

## Hub/relay feasibility — decisive

**The hub already supports it; no server/protocol change is required.**

- Session key is `(projectID, instanceID)` (`service.go:327-329`); `Register`
  replaces only on a full-key collision (`service.go:332-349`). Two sessions with
  the same `instanceID` but different projects coexist.
- Project identity is the connection's own project-scoped bearer token
  (`handler.go:50`, `middleware.go:82-85`); the register frame carries no project
  id (`internal/relay/frames.go:33-39`). A second connection uses a second token
  and lands under the second project's key.
- Per-project token minting already avoids hostname collisions:
  `internal/project/project.go:372-378` names the token
  `connector-<hostname>-<projectID>`.

## Options

### (a) One multiplexed engine process serving N registrations
One process, one port, a `projects:` config list, N relay clients + N registries
+ N `mcphost.Manager`s in-process, project-scoped management API.
- Pros: one process/port.
- Cons: large engine rewrite (client fan-out, per-project registries/mcphost/
  managed appliers, project-scoped `mgmtapi`), breaking config schema, **no
  failure isolation** (one crash/OOM/auto-update takes down all projects), and
  the per-process circuit breaker can no longer bound one project independently.

### (b) N engine processes, one per connected project — **CHOSEN**
One engine child per project, each `relay --config <per-project.yml> --api-port
<per-project-port>`.
- Pros: **zero hub/protocol change**; **zero engine rewrite** (binary reused);
  per-project **failure isolation**; per-child circuit breaker already correct;
  config schema unchanged (N files via `MaterializeWithOptions`); single-project
  behaviour byte-identical to today (legacy path + port `8931`).
- Cons: N processes and N loopback ports — bounded bookkeeping localized to the
  Mac supervisor and the MCP-servers client.

**Decision:** option (b). The dominant non-functional requirement — one project's
failure must not affect the others — is only met by per-project processes, and the
engine already implements every per-project behaviour correctly.

## Data model changes

- **Engine config schema: unchanged.** N single-profile config files. The first/
  primary project keeps the legacy path `~/.config/memory-connector.yml`;
  additional projects use a **sibling** file
  `~/.config/memory-connector-<projectID>.yml`. (Implementation finding: the
  sibling layout is required, not a `projects/` subdirectory — the connector CLI
  derives its account-session store from the config file's *parent* directory
  (`account.BaseDirForConfig = <configDir>/memory-connector/`), so a
  subdirectory would orphan the OIDC session and break `projects use` for every
  additional project. Sibling files share the parent, so the session/token store
  stays shared with zero engine change.)
- **`ProjectProfile`**: `connected: Bool` retained but no longer mutually
  exclusive; add `managementPort: Int?`. `setConnected` stops clearing siblings;
  `connectedProjectID()` → `connectedProjectIDs() -> Set<String>`.
- **`ProjectStore`**: `connectedProjectID: String?` → `connectedProjectIDs: Set<String>`;
  `hasConnectedProject` → non-empty set; `connect` adds; add
  `disconnect(projectID:)`; `disconnectAll()` for scope teardown.
- **`EngineLifecyclePolicy`**: unchanged in shape, evaluated **per project**
  (`wrongProject`/`missingConfig` scoped to one project's child).

## Engine lifecycle

Replace the single `syncEngineWithConnection()` with a set reconcile: ensure a
child per desired project (bound to that project's config + port); stop children
not desired. First project keeps port `8931`; additional projects get probed,
persisted ports (`ManagementPortProbe`). `EngineManager` becomes
`children: [ProjectID: ChildSupervisor]`, each reusing the existing
`RestartPolicy`/launch/stop/bind-failure logic. The scope-swap gate and settle
chain generalize to reconcile the whole set once per scope change. `StatusMonitor`
polls per child and publishes an aggregate.

## Per-project sessions/tokens

Project tokens are already per-project and long-lived (`project.go:233-286`). The
account (OIDC) session is per **server** (`internal/account/account.go`), already
N-server capable. One project's auth failure marks only that project; others stay
connected (generalizes the existing "session expires while connected" rule).
Switching accounts stops all children and reconciles the new account's set;
cross-account/environment server-binding checks are unchanged.

## Tools / collisions

Built-in Apple tools are identical per project; the hub attributes them per
`(project, instance)` session, so no cross-project collision. Hosted MCP tools
are already namespaced per process (`<server>_<tool>`, `<instanceID>_<server>_<tool>`)
and now per process/config. The only local concern is N loopback ports.

## UI

Footer/status → "N projects connected"; project switcher lights each connected
project; the connection control becomes per-project connect/disconnect (the
"connecting here will move the connection" note is removed); hosted MCP servers
page becomes project-scoped (talks to the active project's child/port).

## Risks & edge cases

- **Port allocation** is the primary new hazard: probe, persist, and surface a
  held port per project (others keep running).
- **Partial failure** must be isolated and tested (one project's token/relay/auth
  failing while others stay up).
- **Ordering**: concurrent connect/disconnect serialized per child; account swap
  stops all before reconciling.
- **Resource bounds**: no hard cap; each project ≈ one lightweight Go process +
  one WS + one listener. Practical ceiling is OS process/FD/port limits; may warn
  above a soft threshold without enforcing.
- **Logs**: per-child output must be attributable (per-project file or prefixed
  lines), extending "engine lifecycle is observable in the log".
- **Compatibility**: existing single-connection state reads as a one-element set;
  legacy config path + `8931` unchanged for single-project users.

## Implementation notes (as built)

- **Per-project config path** — sibling files
  `~/.config/memory-connector-<projectID>.yml` beside the legacy
  `~/.config/memory-connector.yml` (see the finding above), not a `projects/`
  subdirectory.
- **Per-project managed MCP set** — the engine keys its managed-MCP-server
  persistence on the config file's **identity** (basename), not its parent
  directory: the primary `memory-connector.yml` keeps the legacy
  `<configDir>/memory-connector/managed/` directory, while each per-project
  `memory-connector-<projectID>.yml` gets `<configDir>/memory-connector/managed-<projectID>/`
  (`apps/connector.linux/cmd/memory-connector/managed_config.go`). Sibling configs
  therefore hold **independent** managed sets while the account-session store
  (`<configDir>/memory-connector/`, derived from the parent directory) stays
  shared. Resolves the former shared-engine limitation (#1727); no Mac/hub change
  is required — the engine derives isolation from the `--config` path it is
  already handed.

- **Registry browse surface** — the MCP registry *browse* sheet
  (`MCPRegistryBrowseSheet`/`MCPRegistryAPIClient`) is project-scoped too: it
  re-points its client at the active project's management port when the sheet
  opens and whenever the active project changes, mirroring `MCPServersPage`.
  Resolved in #1728.

## Spec impact

- `mac-connector-app`: "Embed and supervise the connector engine",
  "Require sign-in and connect a project", "Signed-out state prompts
  re-authentication", "Reconcile the engine once per account scope change",
  "Show connection and tool status" — singular → set; plus a new "multiple
  simultaneous connections" requirement.
- `mcp-connector`: "Connect and register with the project relay hub" — multiple
  simultaneous registrations, replacement scoped per project.
