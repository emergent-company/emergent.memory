## Why

When an orchestrator spawns sub-agents, each run gets its own `AgentRun` ID and its own OTel span with no shared identifier linking them back to the top-level trigger. This makes it impossible to aggregate LLM costs across a full orchestration tree or view all agent spans as a single trace in Tempo/Jaeger — both of which are required foundations for run-level budget enforcement.

## What Changes

- Add `root_run_id` field to `ExecuteRequest`; the top-level executor sets it to its own run ID, and every subsequent `spawn_agents` call propagates it unchanged
- Add `root_run_id` context key in the `provider` package alongside the existing `run_id` key, so `TrackingModel` can attach it to every `LLMUsageEvent`
- Add `root_run_id` column to `kb.llm_usage_events` (new Goose migration)
- Add `emergent.agent.root_run_id` attribute to every `agent.run` OTel span so all spans in an orchestration are queryable by a single ID in Tempo/Jaeger

## Capabilities

### New Capabilities

- `root-run-id-propagation`: a `root_run_id` is established at the top-level agent trigger and propagated through all sub-agent spawns, context keys, usage events, and OTel spans — making the full orchestration tree addressable by a single identifier

### Modified Capabilities

- `agent-execution`: `ExecuteRequest` gains a `RootRunID` field; execution entry points (`Execute`, `ExecuteSubAgent`, `Resume`) set or forward it
- `agent-run-tracing`: every `agent.run` span gains the `emergent.agent.root_run_id` attribute
- `llm-cost-tracking`: `LLMUsageEvent` gains a nullable `root_run_id` column; the tracking model writes it from context on every recorded event

## Impact

- **`apps/server/domain/agents/executor.go`** — set `RootRunID` at top-level entry, forward through `runPipeline` span attributes and provider context
- **`apps/server/domain/agents/coordination_tools.go`** — pass `RootRunID` through `CoordinationToolDeps` and into every spawned `ExecuteRequest`
- **`apps/server/domain/provider/context.go`** — add `ContextWithRootRunID` / `RootRunIDFromContext`
- **`apps/server/domain/provider/tracking_model.go`** — read `RootRunID` from context and attach to `LLMUsageEvent`
- **`apps/server/domain/provider/entity.go`** — add `RootRunID *string` field to `LLMUsageEvent`
- **`apps/server/migrations/`** — new Goose migration adding `root_run_id uuid` (nullable) to `kb.llm_usage_events`
- No API surface changes; no breaking changes
