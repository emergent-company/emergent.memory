## 1. Specs & tests first (TDD)

- [x] 1.1 Land the delta spec: `specs/agent-delegation/spec.md` (ADDED internal-unreachability requirement)
- [x] 1.2 Fail-first test: `TestBuildAgentCatalog_ExternalCallerHidesInternal` and `TestSpawnTargetBlocked_ExternalCallerRejectsInternal` fail under open (pre-fix) semantics
- [x] 1.3 Regression tests: internal/project callers still coordinate; spawn-policy allowlist still filters; external callers still spawn external/project targets

## 2. Server — coordination visibility gate

- [x] 2.1 `coordination_tools.go`: add `CoordinationToolDeps.CallerVisibility`, `canReachInternal`, `callerVisibility`; extract `buildAgentCatalog` and `spawnTargetBlocked`
- [x] 2.2 `coordination_tools.go`: `list_available_agents` hides internal for external callers; `executeSingleSpawn` rejects internal targets for external callers
- [x] 2.3 `executor.go`: `buildCoordinationTools` populates `CallerVisibility` from `req.AgentDefinition`

## 3. Verify

- [x] 3.1 `go build ./...`
- [x] 3.2 `go test ./domain/agents/ -run 'TestCanReachInternal|TestCallerVisibility|TestBuildAgentCatalog|TestSpawnTargetBlocked'`
- [ ] 3.3 `go test ./domain/agents/...` against hermetic Postgres (REQUIRE_DB=1)
- [ ] 3.4 `bash scripts/lint-ratchet.sh`
- [ ] 3.5 `golangci-lint` on touched packages
- [ ] 3.6 `openspec validate --all --strict`
