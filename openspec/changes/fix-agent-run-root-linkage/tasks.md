## 0. Recon

- [x] 0.1 Live/dev evidence of the gap: `SELECT count(*), count(root_run_id) FROM
      kb.agent_runs` → root count 0; child runs spawned via `spawn_agents` have
      `parent_run_id` set but `root_run_id` NULL (5/5).
- [x] 0.2 Root establishment happens in memory correctly in `Execute`
      (`executor.go:454`) and `ExecuteWithRun` (`:641`), but the DB write is
      inside `if sc := span.SpanContext(); sc.IsValid()` — with the default noop
      tracer (`domain/tracing/module.go:47`) the span is never valid.
- [x] 0.3 `CreateRunWithOptions` / `CreateRunOptions` have no root field, so the
      insert cannot carry it; the `getRootRunID` comment claiming otherwise is
      stale.
- [x] 0.4 `CreateRunQueuedOptions` has no root, and the worker-pool re-enqueue
      path (`worker_pool.go:319`) plus `mcp_tools.go:564` pass none, so a
      re-enqueued run self-roots to its own new id via `ExecuteWithRun`.
- [x] 0.5 In-memory propagation is already correct and unchanged:
      `coordination_tools.go:393-408` passes `deps.RootRunID`, populated at
      `executor.go:2997-3008`.
- [x] 0.6 Consumers identified: `getRootRunID`, ADK session-id derivation,
      `provider.ContextWithRootRunID` → `kb.llm_usage_events.root_run_id`, and the
      in-flight `agent-run-preview` change (groups the delegation tree by root
      run).
- [x] 0.7 `agent-delegation`'s spec has no run-linkage requirement to modify —
      this is an ADDED requirement under a new capability.

## 1. Implementation

- [x] 1.1 `entity.go` — `CreateRunOptions` and `CreateRunQueuedOptions` gain
      `RootRunID *string`.
- [x] 1.2 `repository.go` — `CreateRunWithOptions` and `CreateRunQueued` persist
      the root column at insert.
- [x] 1.3 `executor.go` — `Execute` passes the known root into run creation; both
      `Execute` and `ExecuteWithRun` resolve the root via the new
      `resolveRootRunID` (caller override → stored root → own id).
- [x] 1.4 `executor.go` — new `persistRunLinkage` writes `root_run_id`
      unconditionally and keeps `trace_id` gated on a valid span; both previous
      `if sc.IsValid()` write sites now call it. Failures remain logged warnings.
- [x] 1.5 `mcp_tools.go` — the queued branch carries
      `provider.RootRunIDFromContext(ctx)` into the queued run.
- [x] 1.6 `worker_pool.go` — `reenqueueParent` propagates
      `parentRun.RootRunID`, so a re-enqueue no longer splits the tree.
- [x] 1.7 `executor.go` — stale `getRootRunID` comment corrected.
- [x] 1.8 No schema change: the column and migration `00060` already exist.

## 2. Verification

- [x] 2.1 `agent_run_root_test.go` (new, DB-free) covers resolution precedence —
      top-level self-root, child inheriting the parent root, override winning,
      re-enqueued run keeping its stored root, and nil/empty edge cases.
- [x] 2.2 Persistence is asserted with the capture-connection driver pattern
      (`ask_user_tool_test.go`): with an **invalid** span the root is still
      written and `trace_id` is NULL (the regression this change fixes); with a
      valid span both are written.
- [x] 2.3 Both insert paths are asserted to carry `root_run_id`.
- [x] 2.4 `go build ./...` passes in `apps/server`.
- [x] 2.5 `go test ./domain/agents/...` passes.
- [x] 2.6 Lint (`golangci-lint run ./domain/agents/...`) reports no new findings
      in the touched files; the 15 reported issues are pre-existing elsewhere in
      the package.
- [ ] 2.7 End-to-end proof after the fix is deployed to dev: re-run
      `apps/web-ui/tests/e2e/scenarios/agent-delegation.spec.ts` and confirm the
      child run now carries a root
      (`SELECT count(root_run_id) FROM kb.agent_runs WHERE parent_run_id IS NOT NULL` > 0).
      Not runnable before deploy — the gateway proxies the deployed dev image.

## 3. Ship

- [ ] 3.1 Commit the change directory and the implementation together and open
      one PR against `main`.
- [ ] 3.2 After merge, `openspec archive fix-agent-run-root-linkage` and sync the
      delta into `openspec/specs/agent-run-linkage/`.

## 4. Follow-ups (out of scope)

- [ ] 4.1 No backfill of historical rows; their roots stay NULL. A backfill would
      have to infer roots from `parent_run_id` / `resumed_from` chains across
      arbitrary history with no way to validate the result.
- [ ] 4.2 Once this is deployed and the delegation scenario passes with a root
      present, tighten `agent-delegation.spec.ts`: the `rootRunId` assertion there
      is currently a cross-check (it cannot require a root until this fix ships).
- [ ] 4.3 `agent-run-preview`'s claim that "no memory-service code changes are
      needed" was wrong; its grouping-by-root design assumes this fix. Worth
      correcting in that change rather than here.
