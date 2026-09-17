# Design — fix-agent-run-root-linkage

## Context

`kb.agent_runs.root_run_id` is meant to answer "which orchestration does this run
belong to?". A top-level run roots to itself; a spawned sub-agent inherits its
delegator's root. Consumers:

- `executor.go` `getRootRunID` — falls back to walking `resumed_from` when the
  column is empty, then to the run's own id;
- `executor.go` `runPipeline` — the ADK session id is the orchestration root;
- `provider.ContextWithRootRunID` → `kb.llm_usage_events.root_run_id` — cost
  attribution across a delegation tree;
- the in-flight `agent-run-preview` change — "grouped by root run", "all runs in
  that tree share a common root run identifier".

Today the column is NULL for every run. Root establishment happens in memory
correctly (`if req.RootRunID == nil { req.RootRunID = &run.ID }`), but the write
to the database sits inside `if sc := span.SpanContext(); sc.IsValid()`. Tracing
is opt-in (`OTEL_EXPORTER_OTLP_ENDPOINT` unset installs a noop provider), so the
span is never valid, the `UPDATE` never runs, and the column keeps the NULL
default from migration 00060. `CreateRunWithOptions` cannot compensate: it has no
root field, despite `getRootRunID`'s comment claiming root is stored at creation.

A second hole is independent of tracing: `CreateRunQueuedOptions` has no root, so
a queued/re-enqueued run is executed with no root and, through
`ExecuteWithRun`, self-roots to its own new run id — splitting a delegation tree
the re-enqueue existed to preserve.

`Resume` (`executor.go:867`) already persists root unconditionally; it is the
existing precedent for the fix.

## Decisions

### D1 — Root persistence is unconditional; only `trace_id` is tracing-gated

The orchestration root is a logical relationship between runs. It exists whether
or not anyone is collecting traces, and the consumers above read it precisely
once, from the row. `trace_id` is a tracing artifact and genuinely cannot exist
without a span. So `persistRunLinkage` always writes `root_run_id` and writes
`trace_id` as NULL when the span context is invalid, matching what
`UpdateTraceAndRootRun` already does for an empty trace.

### D2 — Write the root at insert when it is already known

`CreateRunOptions` and `CreateRunQueuedOptions` gain `RootRunID`, and both insert
paths persist it. A spawned child (root = delegator's run) and a queued child
(root = caller's root) are therefore born linked, with no window in which the row
exists without a root. The post-insert self-root `UPDATE` remains, because a
top-level run's root is its own id and the id only exists after the insert.

### D3 — Root resolution precedence: caller override → stored root → own id

`resolveRootRunID(run, reqRoot)` encodes that order. The caller override is what
spawns use to propagate the delegator's root unchanged. Reading the stored root
is what stops a re-enqueued run from self-rooting to its new id — the exact
behaviour that split trees before. Self-rooting is the last resort for a genuine
top-level run.

### D4 — Propagate the root through both dispatch modes

`ExecuteTriggerAgent` reads the caller's root from context once
(`rootOverrideFromContext(ctx)`) and passes it to whichever branch runs: the
queued branch copies it into `CreateRunQueuedOptions`, and the sync branch — the
default dispatch mode — sets it on `ExecuteRequest`. Propagating only to the
queued branch would leave the default path self-rooting its child and splitting
the tree. `WorkerPool.reenqueueParent` also passes `parentRun.RootRunID` through,
so combined with D3's stored-root fallback a run that survives a queue hop keeps
its original root even if the context carries none.

### D7 — Normalize an empty root to NULL at the create boundary

`resolveRootRunID` treats `&""` as absent, so the value that reaches
`CreateRunOptions.RootRunID` / `CreateRunQueuedOptions.RootRunID` must be
normalized the same way: `nilIfEmpty` maps nil or `""` to nil on both insert
paths. Without it an empty override is written as `''` into a `uuid` column and
fails run creation — a failure mode that did not exist before the root was
inserted at all. `rootOverrideFromContext` applies the same rule on the read
side, so the delegation tool never builds a pointer to `""`.

### D5 — No backfill

Existing rows keep NULL roots. Backfilling would require inferring a root from
`resumed_from` / `parent_run_id` chains for arbitrary history, with no way to
validate the result; the value is observability metadata, and consumers already
tolerate its absence. Out of scope, recorded as a follow-up.

### D6 — Persistence failures stay warnings

Run linkage is metadata: a failed write must not fail an agent run. The three
write sites keep logging a warning, as before.

## Risks

- **Consumers see a field that was previously absent.** `rootRunId` is
  `*string` with `omitempty`, so environments with tracing off start emitting it.
  That is the fix; no API shape changes.
- **No schema change.** The column and migration exist; nothing to roll forward.
- **Queued-run fallback depends on the stored root.** When a queued run is
  created without a root (no context value) it is inserted with NULL and, on
  execution, resolves to its own id — the pre-fix behaviour, not a regression.

## Verification

- DB-free unit tests in `apps/server/domain/agents/agent_run_root_test.go` using
  the capture-connection driver pattern from `ask_user_tool_test.go`: resolution
  precedence (top-level, child, override, re-enqueued, empty/nil), persistence
  with an invalid span (root written, trace NULL — the regression), persistence
  with a valid span (both written), and both insert paths carrying the column.
- `go build ./...` and `go test ./domain/agents/...` pass; lint reports no new
  findings in the touched files.
- A full end-to-end `Execute`/`ExecuteWithRun` test is not feasible DB-free (ADK
  model + pipeline + concrete `*Repository`), so each behaviour is tested at its
  narrowest unit and the end-to-end proof is the live delegation scenario once
  the fix is deployed: re-run
  `apps/web-ui/tests/e2e/scenarios/agent-delegation.spec.ts` and confirm the child
  run now carries a root (`SELECT count(root_run_id) FROM kb.agent_runs WHERE
  parent_run_id IS NOT NULL` > 0).
