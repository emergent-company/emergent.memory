## 1. Design (this change)

- [x] 1.1 Recon the engine (Go), relay client, hub (`mcprelay`), and Mac app; identify every single-connection assumption with file:line references.
- [x] 1.2 Confirm hub feasibility: sessions are keyed `(projectID, instanceID)` and project identity comes from the token, so no server/protocol change is needed.
- [x] 1.3 Decide the engine architecture: **N engine processes (one per connected project)** over one multiplexed process, for per-project failure isolation and zero engine rewrite.
- [x] 1.4 Write `proposal.md`, `design.md`, and the delta specs for `mac-connector-app` and `mcp-connector`.
- [x] 1.5 Review/approve the design (this PR).

## 2. Engine / CLI (Go — `apps/connector.linux`)

- [x] 2.1 Confirmed the engine binary is reused as-is; no per-project config-path helper needed once config paths are siblings (see 2.3).
- [x] 2.2 Confirmed `MaterializeWithOptions` + the managed-config path are per config file (no schema change).
- [x] 2.3 Confirmed `projects use` honours an explicit per-project config path; additional projects use sibling config files (`~/.config/memory-connector-<projectID>.yml`) so the CLI's account-session store stays shared.
- [x] 2.4 `go build ./...` PASS; `go test ./...` PASS (the only failure is the pre-existing, macOS-only `internal/appletools` Reminders test on Linux).

## 3. Mac app (Swift — builds/tests only on the Mac build machine)

- [x] 3.1 `ProjectProfile`/`ProjectProfileStore`: `connectedProjectIDs()` set; dropped mutual-exclusion in `setConnected`; added per-project `managementPort`.
- [x] 3.2 `ProjectStore`: `connectedProjectIDs: Set<String>`; `connect` adds; added `disconnect(projectID:)` + `disconnectAll()`.
- [x] 3.3 `EngineLifecyclePolicy`: evaluated per project (unchanged shape).
- [x] 3.4 `EngineManager`/`ChildSupervisor`: `children: [ProjectID: ChildSupervisor]`; per-project config path + probed/persisted management port; per-child restart/stop.
- [x] 3.5 `AppEnvironment`: set-reconcile replaces the single `syncEngineWithConnection`; scope-swap gate generalized to the set.
- [x] 3.6 `StatusMonitor`/`AppStatus`: per-child polling → aggregate; footer "N projects connected".
- [x] 3.7 `ProjectConnectionControl` + Project & Account page: per-project connect/disconnect; single-connection copy removed.
- [x] 3.8 `MCPServersStore`/`MCPServersAPIClient`/`MCPServersPage`: project-scoped, per-project base URL/port.
- [x] 3.9 Legacy migration: an existing single connected project reads as a one-element set; legacy config path + port `8931` unchanged.
- [x] 3.10 Per-child logging is attributable (primary → shared app log; others → `memory-connector-app-<projectID>.log`).
- [x] 3.11 Unit tests updated/added; full suite green on the Mac build machine; SwiftFormat + SwiftLint ratchet clean.

## 4. Spec + archive

- [x] 4.1 Delta specs match shipped behaviour (config-path finding recorded in `design.md`).
- [x] 4.2 `openspec validate mac-connector-multi-project --strict` passes.
- [ ] 4.3 Archive post-merge: `openspec archive mac-connector-multi-project`.
