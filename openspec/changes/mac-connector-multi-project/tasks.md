## 1. Design (this change)

- [x] 1.1 Recon the engine (Go), relay client, hub (`mcprelay`), and Mac app; identify every single-connection assumption with file:line references.
- [x] 1.2 Confirm hub feasibility: sessions are keyed `(projectID, instanceID)` and project identity comes from the token, so no server/protocol change is needed.
- [x] 1.3 Decide the engine architecture: **N engine processes (one per connected project)** over one multiplexed process, for per-project failure isolation and zero engine rewrite.
- [x] 1.4 Write `proposal.md`, `design.md`, and the delta specs for `mac-connector-app` and `mcp-connector`.
- [ ] 1.5 Review/approve the design (this PR).

## 2. Engine / CLI (Go — `apps/connector.linux`)

- [ ] 2.1 Confirm the engine binary is reused as-is; add a per-project config-path helper only if needed (multi-project = N single-profile config files).
- [ ] 2.2 Verify `MaterializeWithOptions` + the managed-config path already isolate per config file (no schema change).
- [ ] 2.3 Verify `projects use` honours an explicit per-project config path.
- [ ] 2.4 `go build ./...` + `go test ./...` (config, project, relay, mcphost, mgmtapi, status).

## 3. Mac app (Swift — builds/tests only on the Mac build machine)

- [ ] 3.1 `ProjectProfile`/`ProjectProfileStore`: `connectedProjectIDs()` set; drop mutual-exclusion in `setConnected`; add per-project `managementPort`.
- [ ] 3.2 `ProjectStore`: `connectedProjectIDs: Set<String>`; `connect` adds; add `disconnect(projectID:)`; keep `disconnectAll()`.
- [ ] 3.3 `EngineLifecyclePolicy`: evaluate per project (unchanged shape).
- [ ] 3.4 `EngineManager`: `children: [ProjectID: ChildSupervisor]`; per-project config path + probed/persisted management port; per-child restart/stop.
- [ ] 3.5 `AppEnvironment`: set-reconcile in place of the single `syncEngineWithConnection`; generalize the scope-swap gate/settle to the set.
- [ ] 3.6 `StatusMonitor`/`AppStatus`: per-child polling → aggregate; footer "N projects connected".
- [ ] 3.7 `ProjectConnectionControl` + Project & Account page: per-project connect/disconnect; remove the "moving the connection" note.
- [ ] 3.8 `MCPServersStore`/`MCPServersAPIClient`: project-scoped, per-project base URL/port.
- [ ] 3.9 Legacy migration: an existing single connected project reads as a one-element set; legacy config path + port `8931` unchanged.
- [ ] 3.10 Per-child logging is attributable (per-project file or prefixed lines).
- [ ] 3.11 Unit tests (Mac build machine): `EngineLifecyclePolicyTests`, `EngineManagerPortProbeTests`, `ProjectStore*Tests`, `ProjectProfileStoreTests`, `MCPServersAvailabilityTests`, `StatusSnapshot*Tests`; lint via `Scripts/lint-swift.sh`.

## 4. Spec + archive

- [ ] 4.1 Confirm the delta specs match shipped behaviour.
- [ ] 4.2 `openspec validate mac-connector-multi-project --strict`; `task openspec:archive-check` stays green.
- [ ] 4.3 Archive post-merge: `openspec archive mac-connector-multi-project`.
