## 1. Specs & tests first (TDD)

- [x] 1.1 Land the delta spec: `specs/agent-delegation/spec.md` (ADDED internal-unreachability-from-external-surfaces requirement)
- [x] 1.2 Fail-first: external-facing gate (list + spawn), nil-definition resume, agentcompat resolution — all fail under pre-fix semantics
- [x] 1.3 Regression: trusted surfaces still list/spawn internal; allowlist still filters; external-facing still spawns external/project targets

## 2. Server — surface signal + gate

- [x] 2.1 `executor.go`: add `ExecuteRequest.ExternalFacing`; thread into `CoordinationToolDeps` in `buildCoordinationTools`
- [x] 2.2 `coordination_tools.go`: replace visibility inference with `ExternalFacing`; `canReachInternal(bool)`; `buildAgentCatalog`/`spawnTargetBlocked` key on the surface
- [x] 2.3 A2A `message:send` + `message:stream` (start + resume): set `ExternalFacing: true`
- [x] 2.4 agentcompat new-run + resume: set `ExternalFacing: true`
- [x] 2.5 agentcompat `HandleChatCompletion`: refuse internal agents at resolution
- [x] 2.6 share `buildShareExecuteRequest`: set `ExternalFacing: true`

## 3. Verify

- [x] 3.1 `go build ./...`
- [x] 3.2 per-hole fail-first tests + preserved-path regressions (`domain/agents`, `domain/agentcompat`)
- [ ] 3.3 `go test ./domain/agents/... ./domain/agentcompat/...` against hermetic Postgres (REQUIRE_DB=1)
- [ ] 3.4 `bash scripts/lint-ratchet.sh`
- [ ] 3.5 `golangci-lint` on touched packages
- [ ] 3.6 `openspec validate --all --strict`

## 4. Transitive reach + fail-closed polarity (#954)

- [x] 4.1 Invert polarity: `ExternalFacing` → `TrustedInternal` (zero value = untrusted)
- [x] 4.2 Persist the marker on `kb.agent_runs.trusted_internal` (migration 00180, default false)
- [x] 4.3 Propagate through child spawn (`executeSingleSpawn`) and resume (`Resume` inherits prior run)
- [x] 4.4 Fail-first DB tests: external→project→internal blocked, suspended external run stays untrusted, trusted delegation preserved, omitted declaration restrictive
