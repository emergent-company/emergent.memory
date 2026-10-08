## 1. Keyed share path (#1648)

- [x] 1.1 `shareRunner` exposes `EnsureExecutionEnabled`; `StreamMessage` refuses before budget reservation and share-run creation with a `503 agent_execution_disabled` error
- [x] 1.2 Add `FailRunWithSteps` to `ShareRepo`; generalise `failRunIfExecutionDisabled` to a `runFailer` interface and reuse it on the post-`Execute` disabled path
- [x] 1.3 Test: no share run created when `AgentSafeguardsConfig.ExecutionEnabled` is false
- [x] 1.4 Test: a mid-flight disabled race marks the pre-created share run terminal with the disabled reason (not `running`)

## 2. Verification

- [x] 2.1 `go build ./...` from `apps/server`
- [x] 2.2 `go vet ./...` from `apps/server`
- [x] 2.3 `go test ./domain/agents/... -count=1` from `apps/server`
- [x] 2.4 `golangci-lint run --new-from-rev=origin/main ./domain/agents/...` (0 new issues)
- [x] 2.5 `openspec validate fix-share-run-kill-switch-gap --strict`
