## Why

The platform records per-operation LLM token usage in `kb.llm_usage_events` (with `estimated_cost_usd`), but these events carry no `agent_run_id` — so token costs cannot be attributed to a specific run. The CLI `memory traces list/get` shows spans and durations but is silent on token counts and costs, making it impossible to evaluate the economic footprint of individual agent runs from the command line.

## What Changes

- Add nullable `agent_run_id UUID FK` column to `kb.llm_usage_events` (Goose migration)
- Propagate the active agent run ID through context so `UsageService` can stamp it on every usage event emitted during a run
- Add a per-run token/cost query to the agents domain (service + store)
- Extend `memory traces list` output columns: `INPUT TOKENS`, `OUTPUT TOKENS`, `EST. COST`
- Extend `memory traces get <id>` to include a token/cost summary block at the top of the span tree
- Note: pricing is sourced from the internal GitHub registry (`emergent-company/model-pricing`), not `models.dev` directly; the daily sync cron job is already implemented

## Capabilities

### New Capabilities
- `run-token-summary-cli`: CLI commands `memory traces list` and `memory traces get` display per-run token totals (input + output) and estimated cost in USD, fetched from `kb.llm_usage_events` attributed to the run

### Modified Capabilities
- `llm-cost-tracking`: Add nullable `agent_run_id` FK column to `kb.llm_usage_events`; propagate run ID from agent executor context so usage events are attributed to the originating run

## Impact

- **DB schema**: `kb.llm_usage_events` gains a nullable `agent_run_id UUID` column with FK to `kb.agent_runs(id)` — requires a Goose migration
- **`apps/server/domain/provider/`**: `UsageService.Track()` call sites need run ID injected via a context key; `tracking_model.go` and agent executor wiring updated
- **`apps/server/domain/agents/`**: New service/store method to sum `(text_input_tokens + image_input_tokens + video_input_tokens + audio_input_tokens)`, `output_tokens`, and `estimated_cost_usd` for a given `agent_run_id`
- **`tools/cli/internal/cmd/traces.go`**: Updated `list` table and `get` detail output to show token/cost data fetched via the server API
- **API**: New endpoint or augmented DTO to expose per-run token/cost summary; authenticated, scoped to project
- **No frontend changes** (UI is out of scope for this change)
