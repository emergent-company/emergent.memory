## Why

Three follow-ups from the OpenAI Responses protocol and models.dev pricing work
(#1455/#1459, #1452). They are independent but small and all live in the
provider/ADK layer, so they ship as one change:

1. **Graded reasoning effort is unreachable (#1462).** The Responses adapter
   implements `ReasoningEffortConfigurator`, but the model production returns is
   wrapped in `TrackingModel`, which does not delegate the interface, and the
   agentcompat `reasoning_effort` request field is never read. So `tools` + a
   graded `reasoning_effort` (including the Responses-only `max`) can never be
   sent.
2. **Reasoning items are dropped on replay (#1463).** The Responses adapter
   omits reasoning items when reconstituting conversation history. OpenAI
   requires a reasoning item and the `function_call` it produced to stay
   contiguous in `input`; dropping it loses reasoning state across multi-step
   tool loops and can 400.
3. **Model limits sync always falls back to static values (#1464).**
   `model_limits_sync.go` filters on a top-level `type == "generative"` field
   the models.dev feed no longer emits, and caps the body at 4 MB while the feed
   is now ~5.3 MB. Either defect forces the static fallback on every run, so
   context limits are stale for new models.

## What Changes

- Plumb a per-run graded reasoning effort end-to-end:
  agentcompat `reasoning_effort` → `agents.ExecuteRequest.ReasoningEffort` →
  the resolved model via `ReasoningEffortConfigurator`.
- Make `provider.TrackingModel` delegate `SetReasoningEffort` to its inner model
  (mirroring the existing `SetEnableThinking` delegation), so the wrapped model
  the executor sees stays configurable.
- Carry the opaque Responses reasoning item id through ADK turn state on the
  generated `function_call` part (via `genai.Part.PartMetadata`) and replay it as
  a `reasoning` input item immediately before that `function_call`.
- Fix the models.dev limits sync: drop the obsolete `type` filter (key off a
  positive `limit.output`) and raise the body cap to 64 MB to match the pricing
  fetcher.
- No database migration.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `openai-compatible-llm`: the graded reasoning effort is now wired from the
  request to the adapter, and reasoning items are replayed across tool loops.
- `multivendor-provider-registry`: the models.dev model-limits sync must apply
  the remote feed rather than always falling back to static limits.

## Impact

- `apps/server/domain/agentcompat/service.go` — pass `reasoning_effort` into both
  the execute and resume `ExecuteRequest`s.
- `apps/server/domain/agents/executor.go` — `ExecuteRequest.ReasoningEffort`;
  apply it to the model in `runPipeline`.
- `apps/server/domain/provider/tracking_model.go` — delegate
  `SetReasoningEffort`.
- `apps/server/pkg/adk/openai_responses_model.go` — carry and replay reasoning
  item ids.
- `apps/server/domain/provider/model_limits_sync.go` — type-less parsing and
  64 MB body cap.
- Tests: tracking-model delegation, Responses reasoning replay, limits parser and
  large-body fetch.
