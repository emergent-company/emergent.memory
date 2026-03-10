## 1. Database Migration

- [x] 1.1 Create `apps/server/migrations/00050_add_run_id_to_llm_usage_events.sql` with `-- +goose Up` adding nullable `run_id uuid REFERENCES kb.agent_runs(id) ON DELETE SET NULL` column to `kb.llm_usage_events`, plus partial index `idx_llm_usage_events_run_id` WHERE run_id IS NOT NULL; and `-- +goose Down` dropping both

## 2. Provider Domain — Context Key & Entity

- [x] 2.1 Add `runIDKey struct{}` context key with exported `ContextWithRunID(ctx context.Context, runID string) context.Context` and `RunIDFromContext(ctx context.Context) string` helpers to `apps/server/domain/provider/` (new file `context.go`)
- [x] 2.2 Add `RunID *string` field to `LLMUsageEvent` struct in `apps/server/domain/provider/entity.go` with bun tag `bun:"run_id,type:uuid,nullzero"`
- [x] 2.3 Update `recordUsage` in `apps/server/domain/provider/tracking_model.go` to call `RunIDFromContext(ctx)` and set `event.RunID` when non-empty

## 3. Agent Executor — Run ID Propagation

- [x] 3.1 In `apps/server/domain/agents/executor.go` `runPipeline`, call `provider.ContextWithRunID(ctx, run.ID)` at the same point `callerRunIDKey` is set (line ~542), updating `ctx` before model creation

## 4. Agents Domain — Token Aggregation

- [x] 4.1 Add `RunTokenUsage` struct to `apps/server/domain/agents/dto.go` with fields `TotalInputTokens int64`, `TotalOutputTokens int64`, `EstimatedCostUSD float64`
- [x] 4.2 Add `TokenUsage *RunTokenUsage` field to `AgentRunDTO` in `apps/server/domain/agents/dto.go` with tag `json:"tokenUsage,omitempty"`
- [x] 4.3 Add `GetRunTokenUsage(ctx context.Context, runID string) (*RunTokenUsage, error)` method to agents repository in `apps/server/domain/agents/repository.go`, querying `SELECT SUM(text_input_tokens + image_input_tokens + video_input_tokens + audio_input_tokens), SUM(output_tokens), SUM(estimated_cost_usd) FROM kb.llm_usage_events WHERE run_id = ?`
- [x] 4.4 Update `AgentRun.ToDTO()` in `apps/server/domain/agents/dto.go` (or the handler) to call `GetRunTokenUsage` and populate `TokenUsage`; return `nil` when the aggregate returns zero rows or all-zero totals

## 5. CLI — traces list Token Columns

- [x] 5.1 In `tools/cli/internal/cmd/traces.go`, update `printTraceTable` to add `INPUT TOKENS`, `OUTPUT TOKENS`, and `EST. COST` columns to the output table
- [x] 5.2 After fetching the trace search results, extract `emergent.agent.run_id` from each trace's root span attributes (iterate `tempoTraceSearchResult` span attributes or `rootSpanAttributes` field if available)
- [x] 5.3 For traces with a run ID, concurrently fetch agent run DTOs via `GET /api/projects/:projectId/agent-runs/:runId` using `errgroup` or `sync.WaitGroup`; populate a `map[traceID]RunTokenUsage`
- [x] 5.4 In `printTraceTable`, render `totalInputTokens`, `totalOutputTokens`, and `estimatedCostUsd` (formatted as `$X.XXXXXX`) for each row; display `—` when no data is available

## 6. CLI — traces get Token Summary & Attribute

- [x] 6.1 In `runTracesGet`, after parsing the OTLP trace response, walk spans to find any `emergent.agent.run_id` attribute; if found, fetch the agent run DTO and print the summary line `Tokens: <N> in / <N> out  Est. Cost: $X.XXXXXX` before the span tree
- [x] 6.2 Add `emergent.agent.run_id` to the list of printed span attributes in `printNode` (alongside existing `http.method`, `http.route`, etc.) in `tools/cli/internal/cmd/traces.go`
- [x] 6.3 Handle graceful degradation: if the run API call returns non-200 or `tokenUsage` is null, skip the summary block without error

## 7. Verification

- [x] 7.1 Apply migration locally (`task migrate`) and confirm `kb.llm_usage_events` has `run_id` column and partial index
- [ ] 7.2 Run a local agent run and confirm `llm_usage_events` rows are written with `run_id` set
- [ ] 7.3 Call `GET /api/projects/:projectId/agent-runs/:runId` and confirm `tokenUsage` object is present in the response JSON
- [ ] 7.4 Run `memory traces list` and confirm token/cost columns appear for agent-run traces
- [ ] 7.5 Run `memory traces get <traceID>` for an agent trace and confirm summary block appears before span tree
- [x] 7.6 Run `task build` to confirm no compilation errors
