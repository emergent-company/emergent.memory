## Context

Agent runs today execute an unbounded number of LLM calls. Token usage and cost are tracked asynchronously in `kb.llm_usage_events` via `TrackingModel` + `UsageService`, but nothing reads those totals back to stop a run that is approaching operator-defined limits.

`GetRunTokenUsage` already exists in `apps/server/domain/agents/repository.go` and returns `TotalInputTokens`, `TotalOutputTokens`, and `EstimatedCostUSD` for a given run ID by querying `kb.llm_usage_events`. No new DB work is required.

The execution pipeline already has a `beforeModelCb` hook (called before every LLM invocation) that performs pre-flight checks for step count, context limits, and pause state. This is the natural insertion point for budget enforcement.

## Goals / Non-Goals

**Goals:**
- Allow operators to set optional per-run `maxCostUSD` and/or `maxTotalTokens` limits on an `AgentDefinition`
- Enforce those limits before each LLM call, aborting the run with a distinct `budget_exceeded` terminal status if either is breached
- Surface the exceeded status in run responses and streaming events via a `budgetExceeded` flag
- Support the `budget:` key in blueprint YAML under each agent's `model:` block

**Non-Goals:**
- Real-time token counting within a partially-executed LLM call (enforcement is pre-call only)
- Per-step, per-tool, or per-user budget limits (only per-run, per-definition)
- Retroactive enforcement against completed runs
- Alerting or notifications when a budget is nearing (enforcement only, not monitoring)
- Guaranteeing exact cost accuracy — estimates may lag due to async flush (see Risks)

## Decisions

### 1. Enforce in `beforeModelCb`, not after each call

**Decision:** Check accumulated usage *before* each LLM invocation rather than after.

**Rationale:** A post-call check would allow one over-budget call to complete before terminating. Pre-call enforcement is conservative and predictable — if the run has already exceeded the limit when the next call is about to start, it stops. This is consistent with how `maxSteps` is enforced.

**Alternative considered:** Enforce after each call. Rejected because it allows one extra call beyond the limit and complicates rollback semantics.

### 2. Use existing `GetRunTokenUsage` — no new query or cache

**Decision:** Call `GetRunTokenUsage(ctx, runID)` inline in `beforeModelCb` on every invocation.

**Rationale:** This is a simple aggregation query on an indexed column (`run_id`). The overhead is acceptable given that `beforeModelCb` already performs multiple DB lookups (pause state, step count). Adding one more read is not a performance concern for typical agent workloads.

**Alternative considered:** Cache the running total in memory. Rejected because it requires thread-safe state in the executor, adds complexity, and the benefit is minimal given query simplicity.

### 3. `budget_exceeded` as a distinct terminal run status

**Decision:** Add `budget_exceeded` as a new value alongside `success`, `error`, `cancelled`, `paused`, `queued`, `failed`.

**Rationale:** Distinguishing this status allows operators and monitoring systems to differentiate "run stopped by policy" from "run failed unexpectedly". It also enables accurate reporting in future cost-management UIs without requiring callers to inspect run metadata for a flag.

**Alternative considered:** Reuse `error` status with a structured error message. Rejected because it loses the semantic distinction and makes programmatic filtering harder.

### 4. Budget config lives on `ModelConfig.Budget` (not top-level `AgentDefinition`)

**Decision:** Nest budget fields inside `AgentDefinition.ModelConfig` as a new `Budget *AgentBudget` struct.

**Rationale:** Budget limits are intrinsically tied to the model invocation — they cap LLM call costs, not other agent activity. Keeping them inside `ModelConfig` is semantically cohesive and mirrors the YAML structure (`model.budget`), which is natural to operators configuring agents.

**Alternative considered:** Top-level `AgentDefinition.Budget` field. Rejected because budget is model-call-centric, and splitting config from model settings would require callers to look in two places.

### 5. Both limit fields are independently optional

**Decision:** `MaxCostUSD` and `MaxTotalTokens` are both `*float64` / `*int64` (pointer types). A nil value means "no limit". Either or both may be set.

**Rationale:** Operators should be able to enforce cost-only, token-only, or combined budgets depending on their use case. Requiring both would be unnecessarily restrictive.

## Risks / Trade-offs

**Async usage flush lag** → Usage events are written asynchronously by `TrackingModel` + `UsageService`. At the moment `beforeModelCb` queries `GetRunTokenUsage`, the most recently completed call's tokens may not yet be committed to `kb.llm_usage_events`.

Mitigation: This is a known, documented trade-off. Budget enforcement is "at least eventually consistent" — the run will be stopped at the next pre-call check once usage is flushed. In practice the lag is sub-second and the overage is at most one extra LLM call. The design doc and spec note this explicitly so operators set limits with margin.

**`budget_exceeded` status requires all consumers to handle a new enum value** → Any code that switch-exhaustively handles run statuses will need updating.

Mitigation: Treat as additive. Existing consumers that handle `error` as a catch-all will silently group `budget_exceeded` runs — acceptable for now. Document the new status clearly.

**Cost estimation accuracy** → `EstimatedCostUSD` from `GetRunTokenUsage` relies on per-model pricing stored in the DB. If pricing data is stale or missing for a model, cost estimates may be inaccurate.

Mitigation: This is pre-existing behavior from `llm-cost-tracking`. Budget enforcement inherits the same accuracy guarantees. Operators using `maxCostUSD` should be aware of this dependency.

## Migration Plan

1. Deploy server changes (no migration required — uses existing `kb.llm_usage_events` schema)
2. `budget_exceeded` status is additive — existing run status handling is unaffected
3. All new fields are optional — existing agent definitions and blueprints continue to work without modification
4. Rollback: remove `Budget` field from `ModelConfig` and the `budget_exceeded` status check in `beforeModelCb`; no DB changes to revert

## Open Questions

- Should `budget_exceeded` runs expose which limit was hit (`cost` vs `tokens`) in the run response, or is the `budget_exceeded` status sufficient?
  - Current decision: status alone is sufficient for v1; a `budgetExceededReason` field can be added in a follow-up.
- Should `GetRunTokenUsage` be called on every `beforeModelCb` invocation or only after step N (e.g., skip the first few calls)? 
  - Current decision: call on every invocation for simplicity and correctness. Optimize later if profiling reveals it as a bottleneck.
