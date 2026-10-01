## 1. API shape

- [x] 1.1 `ReactionConfig.IgnoreAgentTriggered`/`IgnoreSelfTriggered` → `*bool` with `omitempty` in `apps/server/domain/agents/entity.go`. Verify with `go build ./...`.
- [x] 1.2 Mirror the pointer/omitempty shape in `apps/server/pkg/sdk/agents/client.go`.

## 2. Enforcement

- [x] 2.1 Remove the global agent-origin early return from `onEntityEvent`; carry the originating actor into per-agent matching via an internal `handleEvent` path (public `HandleEvent` unchanged, nil actor).
- [x] 2.2 Per-agent gate `shouldTriggerAgentForActor`: agent-originated events skipped unless `ignoreAgentTriggered:false`; self-triggers additionally require `ignoreSelfTriggered:false`. Self identity = agent definition id.
- [x] 2.3 `ConcurrencyStrategy` enforcement: empty/`"parallel"` no-op; `"skip"` drops a trigger when a non-terminal run exists for the same agent + target object (`HasActiveRunForAgentObject`). Key recorded on the run as `trigger_metadata.subjectObjectId`/`subjectObjectType`.

## 3. Tests

- [x] 3.1 Unset everything → agent-originated event produces no run (default preserved).
- [x] 3.2 `ignoreAgentTriggered:false` → agent-originated event from another agent triggers.
- [x] 3.3 Self-trigger skipped by default; allowed when both flags explicit false.
- [x] 3.4 `concurrencyStrategy:"skip"` skips with an active run; empty/`"parallel"` do not.
- [x] 3.5 Non-agent-originated (user/nil actor) event still triggers.
- [x] 3.6 Repository query shape test (sqlmock).

## 4. Consistency sweep

- [x] 4.1 Update `docs/site/go-sdk/reference/agents.md`.
- [x] 4.2 Regenerate Swagger and confirm `scripts/check-swagger-annotations.sh` passes.

## 5. Verify

- [x] 5.1 `go build ./...` and `go test -count=1 ./domain/agents/... ./domain/scheduler/...`.
- [x] 5.2 `scripts/lint-ratchet.sh` (no regression).
- [x] 5.3 `openspec validate enforce-reaction-config --strict`.
