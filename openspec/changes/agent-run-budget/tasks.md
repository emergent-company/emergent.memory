## 1. Entity — Budget Types and ModelConfig

- [ ] 1.1 Add `AgentBudget` struct to `apps/server/domain/agents/entity.go` with `MaxCostUSD *float64` and `MaxTotalTokens *int64` fields (JSON-tagged `maxCostUSD` / `maxTotalTokens`)
- [ ] 1.2 Add `Budget *AgentBudget` field to `ModelConfig` struct in `apps/server/domain/agents/entity.go` (JSON-tagged `budget,omitempty`)
- [ ] 1.3 Verify that existing `ModelConfig` JSON round-trips still work when no `budget` key is present (nil pointer serializes as absent)

## 2. Run Status — budget_exceeded

- [ ] 2.1 Add `RunStatusBudgetExceeded = "budget_exceeded"` constant to the run status constants in `apps/server/domain/agents/entity.go` (alongside `success`, `error`, `cancelled`, etc.)
- [ ] 2.2 Add `BudgetExceeded bool` field to the agent run response DTO in `apps/server/domain/agents/dto.go`
- [ ] 2.3 Set `BudgetExceeded: true` in the DTO mapping function when run status is `budget_exceeded`

## 3. Executor — Budget Enforcement in beforeModelCb

- [ ] 3.1 In `apps/server/domain/agents/executor.go`, locate `beforeModelCb` (around line 755) and add a budget guard block after the existing pre-flight checks
- [ ] 3.2 In the guard block: if `def.ModelConfig.Budget` is nil, skip the check entirely
- [ ] 3.3 Call `repo.GetRunTokenUsage(ctx, runID)` to retrieve accumulated usage
- [ ] 3.4 If `Budget.MaxCostUSD != nil` and `EstimatedCostUSD >= *MaxCostUSD`, update run to `budget_exceeded` and return an error to abort the pipeline
- [ ] 3.5 If `Budget.MaxTotalTokens != nil` and `(TotalInputTokens + TotalOutputTokens) >= *MaxTotalTokens`, update run to `budget_exceeded` and return an error to abort the pipeline
- [ ] 3.6 Ensure partial summary is preserved in the run record when budget_exceeded is set (follow the same pattern as the step-limit soft-stop)

## 4. CLI Blueprint — Budget YAML Support

- [ ] 4.1 Add `Budget *AgentBudget` field to the `AgentModel` struct in `tools/cli/internal/blueprints/types.go` with YAML tags `budget,omitempty`
- [ ] 4.2 Add `AgentBudget` struct to `tools/cli/internal/blueprints/types.go` with `MaxCostUSD *float64` (yaml: `maxCostUSD`) and `MaxTotalTokens *int64` (yaml: `maxTotalTokens`)
- [ ] 4.3 In `tools/cli/internal/blueprints/loader.go`, map `AgentModel.Budget` → `entity.ModelConfig.Budget` when building the `AgentDefinition` from YAML
- [ ] 4.4 Verify that a blueprint YAML with no `budget:` key under `model:` loads without error and results in `Budget: nil`

## 5. Verification

- [ ] 5.1 Run `task build` and confirm no compilation errors
- [ ] 5.2 Run `task lint` and fix any linter issues
- [ ] 5.3 Run `task test` and confirm existing tests pass
- [ ] 5.4 Manually trigger an agent run with a very low `maxCostUSD` (e.g., `0.000001`) and verify the run terminates with `status: budget_exceeded`
- [ ] 5.5 Confirm a run with no budget config continues to execute normally
