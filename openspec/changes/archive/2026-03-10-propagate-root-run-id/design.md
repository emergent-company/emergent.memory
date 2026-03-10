## Context

When an orchestrator agent spawns sub-agents, each run creates its own `AgentRun` row and its own OTel span. Nothing links them back to the top-level trigger. The `getRootRunID()` function in `executor.go` exists but only walks the `resumed_from` chain — it has no concept of the spawn tree. Similarly, `LLMUsageEvent` records `run_id` (the immediate run), but there is no `root_run_id` to aggregate cost across a full orchestration.

This means:
- You cannot answer "how much did this orchestration cost?" without manually walking `parent_run_id` chains.
- OTel traces in Tempo/Jaeger are disconnected — no single attribute lets you pull all spans for one top-level trigger.
- Without a shared identifier across the tree, run-level budget enforcement is impossible.

**Relevant existing code:**
- `executor.go`: `ExecuteRequest` (line 80–92) — no `RootRunID` field yet
- `executor.go`: `runPipeline` (line 532) — sets `ContextWithRunID` per run, calls `contextWithCallerRunID`
- `coordination_tools.go`: `CoordinationToolDeps` (line 17–34) — has `ParentRunID`, no `RootRunID`; `executeSingleSpawn` builds `ExecuteRequest` at line 316 with no `RootRunID`
- `provider/context.go`: `ContextWithRunID` / `RunIDFromContext` — mirror pattern to add `RootRunID`
- `provider/tracking_model.go` (line 113): reads `RunIDFromContext`, attaches to `LLMUsageEvent.RunID`
- `provider/entity.go` (line 129): `LLMUsageEvent.RunID *string` — add `RootRunID *string` alongside

## Goals / Non-Goals

**Goals:**
- Establish a `root_run_id` at the top-level execution entry point and propagate it unchanged through all sub-agent spawns
- Attach `root_run_id` to every `LLMUsageEvent` row so total cost for an orchestration can be summed with a single WHERE clause
- Attach `emergent.agent.root_run_id` to every `agent.run` OTel span so all spans in an orchestration are queryable together in Tempo/Jaeger

**Non-Goals:**
- No new API endpoints or UI changes
- No changes to how `parent_run_id` works (the existing parent chain is preserved)
- No aggregation endpoints or reporting queries (that is a separate concern)
- No changes to `getRootRunID()` walk logic — the resumed-from walk is orthogonal

## Decisions

### 1. Propagation mechanism: field on `ExecuteRequest` + context key

**Decision:** Add `RootRunID *string` to `ExecuteRequest`. The top-level entry points (`Execute`, `Resume`) set it to the current run's ID when nil. Sub-agent spawns in `executeSingleSpawn` copy it from `CoordinationToolDeps.RootRunID` into the child `ExecuteRequest` unchanged.

In `runPipeline`, alongside the existing `provider.ContextWithRunID(ctx, run.ID)` call, add `provider.ContextWithRootRunID(ctx, rootRunID)`. This carries the root ID through the entire call stack to the `TrackingModel` without passing it as a parameter everywhere.

**Alternatives considered:**
- *Walk the DB chain at event-record time*: would require a DB read on every LLM call — too expensive and adds latency on the hot path.
- *Derive from OTel trace ID*: OTel trace IDs are not stored in the DB, so cost aggregation queries can't join on them.
- *Store on `AgentRun` entity*: useful for reporting, but not required for the propagation itself. Adds a migration and FK concern without immediate payoff. Deferred.

### 2. `CoordinationToolDeps` carries `RootRunID`

**Decision:** Add `RootRunID string` to `CoordinationToolDeps`. When `runPipeline` builds `CoordinationToolDeps` it populates this field from the resolved root run ID. `executeSingleSpawn` then passes it into the child `ExecuteRequest` as `RootRunID: &deps.RootRunID`.

**Why not thread it through context only?** `executeSingleSpawn` constructs `ExecuteRequest` from `CoordinationToolDeps`, not from context. Context is an implicit channel; making it explicit in the struct keeps the spawn path readable and testable.

### 3. Single migration: add `root_run_id` to `kb.llm_usage_events`

**Decision:** One nullable `uuid` column on the existing table. No schema change to `kb.agent_runs` — storing `root_run_id` there is a nice-to-have for tree traversal queries but not required for cost aggregation (usage events are the cost source of truth).

**Migration is non-blocking:** the column is nullable with no default; existing rows remain valid. The column can be added online without a table lock on Postgres 16.

### 4. OTel span attribute: `emergent.agent.root_run_id`

**Decision:** In `runPipeline`, after resolving the root run ID, call `span.SetAttributes(attribute.String("emergent.agent.root_run_id", rootRunID))` on the current span. No new `StartLinked` API needed — child spans are already structurally linked via OTel parent-child when spawned inline. The attribute allows querying by root ID even across async / resumed runs where the span parent chain may be broken.

**Naming:** follows the existing `emergent.agent.*` namespace already used in executor spans.

### 5. Top-level entry points set `RootRunID` defensively

**Decision:** `Execute` and `Resume` both check `if req.RootRunID == nil { req.RootRunID = &run.ID }` after the run record is created/loaded. This means a caller that already supplies a `RootRunID` (e.g. a future external trigger API) can pass it through, while the normal path always gets a valid value. Sub-agent spawns always receive a non-nil `RootRunID` from the parent's `CoordinationToolDeps`.

## Risks / Trade-offs

- **Partial backfill**: All historical `llm_usage_events` rows will have `root_run_id = NULL`. Queries that aggregate cost by orchestration must handle nulls. Backfilling would require walking the `parent_run_id` chain in SQL — not worth the complexity; document the cutover date instead.
  → *Mitigation*: filter `WHERE root_run_id IS NOT NULL` in aggregation queries; treat NULL as "pre-feature".

- **Resumed runs across the feature boundary**: A run started before the migration and resumed after will have `root_run_id` set on post-resume usage events but not pre-resume ones. This is a minor inconsistency with no functional impact.
  → *Mitigation*: acceptable — cost reporting is best-effort for historical data.

- **`CoordinationToolDeps` struct growth**: Adding `RootRunID` is the third cross-cutting identifier after `ParentRunID` and `ProjectID`. If this pattern continues, consider a `RunContext` sub-struct in the future.
  → *Mitigation*: note in code comment; no action required now.

- **Missing root_run_id if TrackingModel is called outside executor**: The context guard in `recordUsage` already skips events with no tenant context. If called without a root run ID the field will be nil — which is valid and handled.

## Migration Plan

1. Write Goose migration `00NNN_add_root_run_id_to_llm_usage_events.sql`:
   ```sql
   -- +goose Up
   ALTER TABLE kb.llm_usage_events ADD COLUMN root_run_id uuid;

   -- +goose Down
   ALTER TABLE kb.llm_usage_events DROP COLUMN root_run_id;
   ```
2. Run migration on staging, verify zero downtime (no table rewrite on Postgres 16).
3. Deploy updated server binary.
4. Rollback: run `-- +goose Down`, redeploy previous binary. No data loss (column is nullable).

## Open Questions

- Should `kb.agent_runs` also gain a `root_run_id` column for tree-traversal queries? Deferred — the usage event column satisfies immediate cost aggregation needs. Revisit when building a reporting UI.
- Should `ExecuteRequest.RootRunID` be exposed in the trigger API so external callers can group runs into a logical orchestration? Deferred — no current use case.
