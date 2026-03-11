## Why

Agents can run unbounded LLM calls today — there is no way to cap total cost or total tokens consumed in a single run. This makes it impossible to give agent deployments predictable cost envelopes in blueprints or prevent runaway spend from misbehaving agents.

## What Changes

- Add an optional `budget` block to `AgentDefinition.ModelConfig` (and the blueprint `AgentModel` type) with two independently-settable limits: `maxCostUSD` and `maxTotalTokens`.
- Before each LLM invocation in `runPipeline`, query accumulated token/cost totals for the current run and abort (with a distinct `budget_exceeded` terminal status) if either limit is breached.
- Blueprint YAML gains a `budget:` key under each agent's `model:` block so operators can declaratively cap per-run spend.
- The run response and streaming events surface `budgetExceeded: true` when the run is stopped for this reason.

## Capabilities

### New Capabilities

- `agent-run-budget`: Per-run cost and token budget enforcement for agent definitions — fields, enforcement logic, terminal status, and blueprint support.

### Modified Capabilities

- `agent-definitions`: `ModelConfig` gains optional `budget` sub-object (`maxCostUSD`, `maxTotalTokens`).
- `agent-execution`: `runPipeline` enforcement — pre-call budget check in `beforeModelCb`, new `budget_exceeded` run status.

## Impact

- **`apps/server/domain/agents/entity.go`** — new `Budget` struct + field on `ModelConfig`
- **`apps/server/domain/agents/executor.go`** — `beforeModelCb` reads accumulated usage and enforces limits
- **`apps/server/domain/agents/repository.go`** — `GetRunTokenUsage` already exists; called inline during execution
- **`apps/server/domain/agents/dto.go`** — run response includes `budgetExceeded` flag
- **`tools/cli/internal/blueprints/types.go`** — `AgentModel.Budget` field
- **`tools/cli/internal/blueprints/loader.go`** — map `budget:` YAML → entity
- No new DB migrations required (uses existing `kb.llm_usage_events`)
- No breaking changes — all new fields are optional with nil meaning "no limit"
