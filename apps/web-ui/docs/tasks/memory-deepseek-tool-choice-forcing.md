# Memory: force tool calls on DeepSeek + refine reasoner classification

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-mcp-tool-call-e2e](../sessions/2026-09-10-mcp-tool-call-e2e.md)

## What

Align `emergent.memory`'s DeepSeek tool-calling behavior with DeepSeek's documented
semantics and the wider ecosystem (LiteLLM, Pydantic-AI, OpenAI Agents SDK).

1. **Refine `isReasonerModel`** (`apps/server/pkg/adk/openai_model.go`). It is a lowercase
   **substring** list `{"reasoner","deepseek-v4-","deepseek-r1","kvasir"}` and skips
   `tool_choice` for every match. DeepSeek's docs restrict `required`/named tool choices by
   **mode** (thinking on/off), not by model. Model it per-mode / per-request instead:
   `supports_tool_choice_required` and `supports_forced_tool_choice_with_thinking`
   (Pydantic-AI's profile approach), so future non-thinking V4 variants are not
   misclassified.
2. **Actually force the first tool call** when it is legal: memory already sends
   `thinking:{"type":"disabled"}` for DeepSeek when tools are present (which per the docs
   makes `required` legal), yet it still skips `tool_choice`. Either send
   `tool_choice:"required"` in that non-thinking request, or self-heal: on a
   `400 "Thinking mode does not support this tool_choice"`, retry with `auto` (or disable
   thinking and keep `required`). Prior art: Graft / captain-claw / cindy self-healers.
3. **Migrate the model id** off the legacy `deepseek-v4-flash` alias (docs: prefers
   `deepseek-flash`; the `deepseek-v4-*` names are being retired/routed to V4.1-Flash).

No change needed to `reasoning_content` echo-back or the `thinking` field shape — both
already match the docs and ecosystem.

## Why

The MCP tool-call e2e only passes because of an imperative prompt; forcing is unavailable,
so agent tool use on DeepSeek is best-effort. Memory already disables thinking with tools
present, so forcing is one step away once the classification/`tool_choice` interaction is
fixed.

## Depends on

- `litellm-deepseek-forced-tool-choice` — the proxy must accept/forward `required` (or the
  client must self-heal around the observed 400).
- Emergent Memory repo (`apps/server/pkg/adk`, `apps/server/domain/provider`).

## Notes

- Same-request `thinking disabled + tool_choice required` currently still 400s through our
  LiteLLM; confirm against the raw DeepSeek API to isolate proxy vs model.
- Don't drop `tools` for thinking models — DeepSeek V3.2+ supports tool calls in thinking
  mode; only the forced `tool_choice` values are disallowed.
- Existing memory release has `openai_model.go` byte-identical across v0.76.0/v0.77.0; note
  `origin/master` of emergent.memory is stale (predates all DeepSeek logic).
