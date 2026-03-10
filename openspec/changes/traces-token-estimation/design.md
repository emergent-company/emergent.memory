## Context

The platform already stores per-operation LLM token usage in `kb.llm_usage_events` (migration 00039), including input/output token counts broken down by modality and a pre-calculated `estimated_cost_usd`. The daily pricing sync cron job (`provider.PricingSyncService`) is fully implemented and populates `kb.provider_pricing` from an internal GitHub-hosted registry at startup and at 02:00 UTC.

The gap is attribution: `kb.llm_usage_events` has no `agent_run_id` column, so token costs cannot be linked to a specific run. The CLI `memory traces list/get` shows OTLP span trees but no token or cost data, even though the `agent-run-tracing` spec already requires `emergent.agent.run_id` to be emitted as a span attribute.

The run ID is already in context during model inference: `executor.go` injects `callerRunIDKey{}` at line 542 in `runPipeline`, before the LLM model is created (lines 557–572). The `TrackingModel` wraps the LLM and fires `recordUsage()` on every response, but currently only reads `ProjectID` and `OrgID` from context — it never reads the run ID.

## Goals / Non-Goals

**Goals:**
- Attribute every `LLMUsageEvent` to its originating `AgentRun` via a nullable FK
- Expose per-run token totals (total input, total output) and estimated cost via the existing run API endpoints
- Show token counts and estimated cost in `memory traces list` and `memory traces get` CLI output
- No changes to the React frontend (out of scope)

**Non-Goals:**
- Replacing the pricing source — the `emergent-company/model-pricing` GitHub registry is kept as-is
- Token tracking for non-agent LLM calls (embeddings, extraction) — the FK is nullable, so non-run calls are unaffected
- Real-time streaming cost updates mid-run
- Per-step or per-tool-call token breakdown in the CLI (aggregate per-run only)

## Decisions

### Decision 1: Nullable FK on `kb.llm_usage_events` rather than a separate rollup table

**Choice:** Add a single nullable `run_id uuid REFERENCES kb.agent_runs(id)` column to the existing `llm_usage_events` table (migration `00050`).

**Alternatives considered:**
- *Separate `kb.agent_run_token_rollups` table*: Simpler to query but requires a second write path and a reconciliation job for runs that complete without a rollup. The FK approach lets the DB aggregate on demand.
- *Store totals denormalized on `kb.agent_runs`*: Would require updating the agent_runs row as usage events arrive (concurrency complexity), and bakes in a point-in-time snapshot that can't be recomputed.

**Rationale:** The FK approach is one migration, zero extra write logic, and the aggregation query is a simple `GROUP BY run_id` on a small result set (runs rarely have more than a few hundred events).

### Decision 2: Propagate run ID via a new `provider` context key, not by reusing `callerRunIDKey`

**Choice:** Add a new unexported `runIDKey struct{}` in `apps/server/domain/provider/` (following the identical pattern used by `callerRunIDKey{}` in `executor.go` and `userCtxKey{}` in `pkg/auth`). Export `ContextWithRunID(ctx, runID)` and `RunIDFromContext(ctx)` for use by the executor.

**Alternatives considered:**
- *Read `callerRunIDKey{}` directly in `tracking_model.go`*: Would create a dependency from `domain/provider` back into `domain/agents` — a forbidden import cycle (agents already imports provider).
- *Thread run ID as a field on `TrackingModel`*: The model is created once per run; the run ID would need to be known at construction, adding coupling between model factory and agent state. Context is the established pattern.

**Rationale:** A context key in the `provider` package is the lightest-weight approach that avoids import cycles and is consistent with the existing `pkg/auth` pattern. The executor, which already has `run.ID`, calls `provider.ContextWithRunID(ctx, run.ID)` at the same point it sets `callerRunIDKey`.

### Decision 3: Surface token data on the existing run API endpoints rather than a new dedicated endpoint

**Choice:** Augment `AgentRunDTO` with optional `tokenUsage *RunTokenUsage` (containing `totalInputTokens`, `totalOutputTokens`, `estimatedCostUsd`). Populate it with a single aggregate query in the `agents` store, called from `GetProjectRun` and `GetSession`. No new route needed.

```go
type RunTokenUsage struct {
    TotalInputTokens  int64   `json:"totalInputTokens"`
    TotalOutputTokens int64   `json:"totalOutputTokens"`
    EstimatedCostUSD  float64 `json:"estimatedCostUsd"`
}

type AgentRunDTO struct {
    // ... existing fields ...
    TokenUsage *RunTokenUsage `json:"tokenUsage,omitempty"`
}
```

**Alternatives considered:**
- *New `GET /api/projects/:projectId/agent-runs/:runId/usage` endpoint*: Clean separation but requires CLI to make two API calls instead of one. No benefit given the data is always needed together.

**Rationale:** Embedding in the existing DTO is backward-compatible (field is `omitempty`), keeps the CLI to a single API call, and follows the existing pattern of enriching DTOs at query time.

### Decision 4: CLI fetches token data via the agent runs API, not by querying Tempo spans

**Choice:** The CLI `traces list` command extracts `emergent.agent.run_id` from each trace's root span attributes, then calls `GET /api/projects/:projectId/agent-runs/:runId` for each run to fetch token/cost data. `traces get` does the same for the single trace.

**Alternatives considered:**
- *Embed token data in OTLP span attributes*: Would require the executor to wait for usage events to drain before closing the span, breaking the async design of `RecordAsync`. Also couples observability data with billing data.
- *New dedicated CLI endpoint that joins Tempo and usage data server-side*: Cleaner but adds server complexity; the CLI already calls multiple endpoints per command for project context.

**Rationale:** The agent run DTO is the authoritative source for run data. The CLI already knows how to resolve a project context. A secondary lookup per trace result is acceptable given the default page size is 20 results.

## Risks / Trade-offs

**[Risk] `emergent.agent.run_id` span attribute may not be set for older runs or when tracing is disabled**
→ The `agent-run-tracing` spec requires this attribute, but it may not be implemented or may be absent when Tempo is not configured. The CLI must handle missing span attributes gracefully — if no `run_id` is found in the trace, token columns show `—` rather than erroring.

**[Risk] `RecordAsync` drops events when the buffer (size 1024) is full**
→ Already a known limitation of the current design. This change does not make it worse. Token totals for a run may be understated under extreme load, but this is acceptable for estimation purposes.

**[Risk] N+1 queries in `traces list` (up to 20 secondary API calls)**
→ Mitigated by: (a) default limit is 20, (b) calls can be made concurrently in the CLI, (c) the aggregate query is indexed on `run_id`. A future optimization could add a batch endpoint, but it's not needed now.

**[Risk] Migration adds a nullable FK that is never populated for historical runs**
→ Acceptable — `tokenUsage` is `omitempty` in the DTO and the CLI shows `—` when no usage data is found. No backfill needed.

## Migration Plan

1. Write Goose migration `00050_add_run_id_to_llm_usage_events.sql`:
   ```sql
   -- +goose Up
   ALTER TABLE kb.llm_usage_events
       ADD COLUMN run_id uuid REFERENCES kb.agent_runs(id) ON DELETE SET NULL;
   CREATE INDEX idx_llm_usage_events_run_id ON kb.llm_usage_events(run_id)
       WHERE run_id IS NOT NULL;

   -- +goose Down
   DROP INDEX IF EXISTS idx_llm_usage_events_run_id;
   ALTER TABLE kb.llm_usage_events DROP COLUMN IF EXISTS run_id;
   ```
2. Deploy migration before deploying updated server binary (the new column is nullable — old binary is unaffected while migration runs).
3. Update `LLMUsageEvent` entity, add `provider.ContextWithRunID`, wire executor, update store/DTO, update CLI.
4. Rollback: `-- +goose Down` drops column and index. No data loss since the column is new.

## Open Questions

- Should the CLI `traces list` make the secondary run API calls concurrently or sequentially? Concurrent up to 20 is low-risk and faster; sequential is simpler. Recommend concurrent with a `sync.WaitGroup` or `errgroup`.
- Should `RunTokenUsage` be embedded even when all values are zero (a run that made no LLM calls)? Recommend returning `null` (`omitempty`) to distinguish "no LLM calls made" from "data not available".
