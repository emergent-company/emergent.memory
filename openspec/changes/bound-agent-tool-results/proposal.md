# Bound tool results before they re-enter the model context

## Why

Tool results are fed back into the agent's model context verbatim. Nothing bounds
their size, so a single oversized result inflates every subsequent model call and
per-step latency grows with the run.

Observed in dev session `d877c9fe-4319-448f-a5c0-377231f4b07e`, run
`7c80b9fe-3f60-4b40-a217-fa39e38dea9d` (`deepseek-v4-flash`, 8 steps, 354 s):
step 6 waited **87.0 s** for the model after a **361 KB** tool result
(`entity-query` with `field_strategy="full"` capped at 25 rows of ~14 KB, plus
`entity-edges-get`, which has no row limit). Model/planning time was ~269 s
(76%) of wall time versus ~83 s (23%) of tool execution, and the assistant
context grew 3.5k → 65k chars across the run.

What already bounds size is insufficient:

- `truncateProperties` caps each individual property **string** at 4000 chars
  but not the number of rows or edges, so 25 rows × 14 KB is still ~350 KB.
- `entity-query` bounds `limit` under `field_strategy="full"`
  (`MCP_ENTITY_QUERY_FULL_MAX_LIMIT`, default 25) — a row cap, not a byte cap.
- The intra-run tool-result cache (#1192) dedupes **repeated identical** calls;
  it does nothing for a single large result.
- No per-result or total byte bound exists anywhere on the tool-result → model
  path.

## What Changes

- Add a **single, central bound at the tool-result → model boundary** in the
  agent executor's before-model callback: walk `model.LLMRequest.Contents` and
  bound every `FunctionResponse` payload before the model call.
- The bound has two layers: a **per-result cap** (configurable, generous
  default) and a **total budget** across all tool results in one model request
  (oldest elided first, most-recent retained).
- The bound is **honest**: a truncated or elided result carries an explicit,
  actionable marker the model sees, telling it how to narrow
  (`filters`/`fields[]`/`limit`/`offset` or fetch a specific key). No silent
  drop.
- The bound touches **only the model request**. ADK deep-clones session content
  into `LLMRequest.Contents` before the callback, so the persisted transcript
  (`kb.agent_run_tool_calls`, `kb.agent_run_messages`), the session events, and
  the streamed `StreamEventToolCallEnd` UI event all retain the full payload.
- Configuration is added next to the existing `MCP_ENTITY_QUERY_*` guardrails:
  `MCP_TOOL_RESULT_MAX_BYTES` (per-result), `MCP_TOOL_RESULT_MAX_BYTES_OVERRIDES`
  (per-tool), `MCP_TOOL_RESULT_TOTAL_BUDGET_BYTES` (total).
- No database migration.

## Capabilities

### New Capabilities
- `agent-model-context-bounds`: bounds the tool-result payloads that enter the
  model context, with a marked per-result truncation and an oldest-first total
  budget, leaving persisted/streamed payloads intact.

### Modified Capabilities
(none — the MCP tool-result envelope returned to callers is unchanged; this
bounds only what the model sees)

## Impact

- `apps/server/domain/agents/tool_result_bounds.go` (new): `ToolResultBounds`,
  per-result bounding envelope, oldest-first total-budget elision, override
  parsing.
- `apps/server/domain/agents/executor.go`: `AgentExecutor.toolBounds` resolved
  from config and applied in `beforeModelCb`.
- `apps/server/internal/config/config.go`: three `MCPConfig` fields.
- `.env.example`: documented knobs.
- No DB/schema changes, no API/transport changes. HTTP transports never build a
  model request, so they are unaffected.
