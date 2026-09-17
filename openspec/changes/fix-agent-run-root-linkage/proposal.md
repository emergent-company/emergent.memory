## Why

Every agent run is supposed to record the orchestration root it belongs to:
a top-level run roots to itself, a spawned sub-agent inherits its delegator's
root, and consumers use that shared identifier to reassemble a delegation tree.
In practice `kb.agent_runs.root_run_id` is NULL for **every** run:

```sql
SELECT count(*), count(root_run_id) FROM kb.agent_runs;  -- root count = 0
```

The cause is not delegation-specific. Both `Execute` and `ExecuteWithRun`
establish the root in memory and then persist it inside
`if sc := span.SpanContext(); sc.IsValid()`. Tracing is opt-in and off by
default (`OTEL_EXPORTER_OTLP_ENDPOINT` unset installs a noop provider), so the
span is never valid, the `UPDATE` never runs, and the column keeps its NULL
default. `CreateRunWithOptions` cannot help because it has no root field at all,
despite a comment claiming the opposite.

A second, independent hole sits in the queued path: `CreateRunQueuedOptions` has
no root either, so a re-enqueued run is executed with no root and self-roots to
its own new run id, silently splitting a delegation tree that the re-enqueue was
supposed to preserve.

This is invisible to the delegation feature itself — the child still links to its
parent through `parentRunId`, and the live delegation scenario passes — but it
breaks root-based grouping, which the in-flight `agent-run-preview` change
depends on ("grouped by root run", "all runs in that tree share a common root
run identifier").

## What Changes

- **Root persistence is no longer gated on tracing.** `Execute` and
  `ExecuteWithRun` persist `root_run_id` whether or not a valid span exists.
  `trace_id` remains tracing-gated, since it cannot exist without tracing.
- **The root is written at insert when it is already known.** `CreateRunOptions`
  gains a root field and `CreateRunWithOptions` persists it, so a spawned child
  row is born with its root instead of relying on a follow-up update. The
  post-insert self-root update stays for top-level runs, whose root is only known
  once the row has an id.
- **Queued runs keep their root.** `CreateRunQueuedOptions` gains the field and
  the re-enqueue path propagates it, so a re-enqueued run inherits the original
  orchestration root instead of self-rooting.
- **The stale comment** claiming root is stored at creation via
  `CreateRunWithOptions` is corrected.
- No schema change: the column and its migration already exist.
- No change to the JSON contract (`rootRunId` stays optional), to API responses,
  or to persistence failure handling (still a logged warning).

## Capabilities

### New Capabilities

- `agent-run-linkage`: the server-side guarantee that every agent run records the
  orchestration root it belongs to — self-rooted for a top-level run, inherited by
  spawned children, and preserved across queued/re-enqueued execution — so
  root-based grouping of a delegation tree is reliable regardless of whether
  tracing is enabled.

### Modified Capabilities

None. `agent-delegation` has no run-linkage requirement to modify; the behavior
added here is new. The in-flight `agent-run-preview` change consumes it and needs
no edit, though its claim that "no memory-service code changes are needed" was
wrong — that is what this change fixes.

## Impact

- `apps/server/domain/agents/executor.go` — ungate the root write in `Execute` and
  `ExecuteWithRun`; pass the known root into run creation; correct the stale
  comment.
- `apps/server/domain/agents/entity.go` — `CreateRunOptions` and
  `CreateRunQueuedOptions` gain the root field.
- `apps/server/domain/agents/repository.go` — persist the root on insert.
- `apps/server/domain/agents/worker_pool.go` (plus the queued-run creation sites
  in `mcp_tools.go`) — propagate the root through re-enqueue.
- New unit tests in `apps/server/domain/agents/`.
- Existing rows are not backfilled. Their roots stay NULL until they are
  re-executed; history is explicitly out of scope.
- Behaviour change visible to consumers: `rootRunId` starts appearing in run
  payloads on environments where tracing is off. That is the fix, not a
  regression.
