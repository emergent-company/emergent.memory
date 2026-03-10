<!-- Baseline failures (pre-existing): none — clean baseline build -->

## 1. Database Migration

- [x] 1.1 Create `apps/server/migrations/00052_add_root_run_id_to_llm_usage_events.sql` with Up adding nullable `root_run_id uuid` column to `kb.llm_usage_events` and Down dropping it
- [x] 1.2 Verify migration applies cleanly with `task migrate` and rolls back with `task migrate:down`

## 2. Provider Context

- [x] 2.1 Add `rootRunIDKey` struct and `ContextWithRootRunID` / `RootRunIDFromContext` functions to `apps/server/domain/provider/context.go`, mirroring the existing `runIDKey` pattern
- [x] 2.2 Add `RootRunID *string` field to `LLMUsageEvent` in `apps/server/domain/provider/entity.go` with bun tag `root_run_id,type:uuid,nullzero`
- [x] 2.3 In `apps/server/domain/provider/tracking_model.go` `recordUsage`, read `RootRunIDFromContext(ctx)` and attach to `event.RootRunID` when non-empty (parallel to the existing `RunID` attachment at line 113)

## 3. ExecuteRequest and Executor Entry Points

- [x] 3.1 Add `RootRunID *string` field to `ExecuteRequest` struct in `apps/server/domain/agents/executor.go`
- [x] 3.2 In `Execute`: after creating the `AgentRun` record, if `req.RootRunID == nil` set `req.RootRunID = &run.ID`
- [x] 3.3 In `Resume`: after loading the resumed run, if `req.RootRunID == nil` set `req.RootRunID = &run.ID`
- [x] 3.4 In `runPipeline`: after the existing `provider.ContextWithRunID(ctx, run.ID)` call, add `ctx = provider.ContextWithRootRunID(ctx, *req.RootRunID)` (guarded for nil)

## 4. OTel Span Attribute

- [x] 4.1 In `runPipeline`, after setting the root run ID on context, set `span.SetAttributes(attribute.String("emergent.agent.root_run_id", *req.RootRunID))` on the current `agent.run` span

## 5. Coordination Tools — Sub-agent Propagation

- [x] 5.1 Add `RootRunID string` field to `CoordinationToolDeps` struct in `apps/server/domain/agents/coordination_tools.go`
- [x] 5.2 In `runPipeline`, when constructing `CoordinationToolDeps`, set `RootRunID` from `req.RootRunID` (dereference with empty-string fallback)
- [x] 5.3 In `executeSingleSpawn`, set `RootRunID: &deps.RootRunID` on the child `ExecuteRequest` (alongside the existing `ParentRunID` assignment)

## 6. Build and Verify

- [x] 6.1 Run `task build` — confirm zero compile errors
- [x] 6.2 Run `task test` — confirm no regressions in unit tests
- [x] 6.3 Run `task lint` — confirm no linter warnings introduced
