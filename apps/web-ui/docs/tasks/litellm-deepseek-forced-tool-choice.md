# LiteLLM rejects forced tool_choice for DeepSeek thinking models

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-mcp-tool-call-e2e](../sessions/2026-09-10-mcp-tool-call-e2e.md)

## What

Make forced tool calling work (or fail predictably) on the dev LiteLLM proxy for
DeepSeek models, which run in thinking mode by default (`deepseek-v4-flash` →
V4.1-Flash).

- Our proxy returns `400 DeepseekException - "Thinking mode does not support this
  tool_choice"` for `tool_choice:"required"`, **including** when the request also sends
  `thinking:{"type":"disabled"}`.
- DeepSeek's own API reference says `required`/named tool choices are unsupported **in
  thinking mode** and "Disable thinking mode first to use them." So the disabled-thinking
  path *should* be accepted.
- The upstream LiteLLM fix is PR [BerriAI/litellm#37470](https://github.com/BerriAI/litellm/pull/37470)
  (open): downgrade `required`/named → `auto` while thinking is active, with
  `THINKING_ON_BY_DEFAULT_MODELS=("deepseek-v4",)`, and pass through when
  `reasoning_effort="none"`.

## Why

Without a working forced-tool-choice path, agent runs on DeepSeek rely on the model
choosing to call a tool, which is non-deterministic and (as seen in the MCP e2e) can be
skipped even when the tool is offered.

## Depends on

- LiteLLM version deployed for the dev proxy; upstream PR #37470 (or a local pin/patch).
- Oracle: whether our LiteLLM forwards `thinking` from the OpenAI-compatible body.

## Notes

- First verify whether the deployed LiteLLM drops `thinking:{"type":"disabled"}` (proxy
  transformation) or whether the model group forces thinking. If it forwards it, test
  `thinking disabled + tool_choice: required` against the raw DeepSeek API to confirm
  the proxy is the only gap.
- Related: `memory-deepseek-tool-choice-forcing` (client side), `e2e-openai-litellm-live-save`.
