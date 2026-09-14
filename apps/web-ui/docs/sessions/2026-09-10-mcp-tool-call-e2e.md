# 2026-09-10 — MCP tool-call e2e: make the live tool call pass

## Goal

Make `tests/e2e/scenarios/mcp-servers-tool-call.spec.ts` actually prove that an agent
calls an external MCP tool in a live chat turn. The scenario kept self-skipping
("no tool-call signal") and it was unclear whether the tool was offered, whether the
model declined, or whether the test simply failed to observe the call.

## Outcome

**Done.** The scenario passes live (`2 passed`) via PRs **#41, #47, #51, #55**, all merged
to `master`. Three distinct defects were found and fixed; two of them were masking each
other.

- **Root cause A (the real one, previously masked): the test matched the wrong tool name.**
  Memory namespaces external tools as `<slugified-server>_<toolName>` and emits that
  *resolved* id in both the `mcp_tool` SSE event and the DOM chip's `data-tool`
  (chat-stream.js `toolChip`). The test matched the **bare** whitelist value
  (`web_fetch_exa`), so neither signal ever matched — even when the tool **had** been
  called. Fixed in **#55** (suffix match: chip `[data-tool$="_<toolName>"]` + SSE regex).
- **Root cause B: double provider prefix.** `tests/e2e/.env.e2e` sets
  `E2E_SCENARIO_LLM_MODEL=openai/deepseek-v4-flash` (already prefixed), while the spec
  prepended `${PROVIDER}/` → `openai/openai/deepseek-v4-flash`. Memory strips **exactly
  one** routing prefix (`stripModelPrefix`), so litellm received the still-prefixed
  `openai/deepseek-v4-flash` → `403 key_model_access_denied`. Fixed in **#47**; the env
  example and its convention documented in **#51**.
- **Root cause C: run errors were mislabeled.** A turn that died before the model ran was
  reported as "the backend did not offer the tool". Fixed in **#41**: parse memory's
  `error` SSE event and skip with the real provider/environment reason.

Also produced an alignment review of memory vs the DeepSeek tool-calling docs and how
other OpenAI-compatible agents handle it (see Open questions).

## Decisions

- **Match the resolved (namespaced) tool id, never the bare whitelist value** — memory
  emits `<slugified-server>_<toolName>` in the event and chip; the bare name never appears.
- **Treat `E2E_SCENARIO_LLM_MODEL` as the already-prefixed `provider/model` catalog
  value** — `skill-execution` asserts the `/`, `blueprint-object-chat` uses it prefixed;
  only this spec double-prefixed. Prepend `PROVIDER/` only for a bare value.
- **Force the call with an imperative prompt + `temperature: 0`** — memory skips
  `tool_choice` for `deepseek-v4-*` reasoner models and the LiteLLM proxy rejects
  `tool_choice:"required"` for them, so the prompt is the only available lever.
- **A clean turn with no tool call skips; a run/provider error surfaces the real reason** —
  do not mask environment failures as model behavior.
- **Merged the PRs while CI was red** — all GitHub Actions jobs fail to start on GitHub
  org billing (`ci-actions-billing-blocker`); merged on the interim "author merges own
  green (locally verified) PR" rule.

## Changes

- `tests/e2e/scenarios/mcp-servers-tool-call.spec.ts` — (#41) parse the `error` SSE event
  and skip with the real provider reason; (#47) build the agent model as `AGENT_MODEL`
  from the prefixed env value, prepending `PROVIDER/` only when bare; (#55) suffix-match
  the resolved tool name for the chip and the SSE body, imperative system/user prompt,
  `temperature: 0`, add `escapeRegExp`, fix the final assertions.
- `tests/e2e/.env.e2e.example` — (#51) `E2E_SCENARIO_LLM_MODEL=openai/deepseek-v4-flash`
  (prefixed), add the embedding-model line, and note that memory strips exactly one
  routing prefix.

No product code changed; all changes are test/env-example docs.

## Verification

- `npx playwright test scenarios/mcp-servers-tool-call.spec.ts --project=scenarios` — **2 passed**
  (tool chip + `mcp_tool` event observed, real Exa result returned).
- Memory-direct repro (fresh agent + MCP server + `POST /api/chat/stream` with the exact
  spec prompt) — SSE carried `mcp_tool started/completed` for `<slug>_web_fetch_exa`.
- Direct LiteLLM probes — `tool_choice:"required"` → `400 DeepseekException - "Thinking
  mode does not support this tool_choice"` (also with `thinking:{"type":"disabled"}`);
  without `tool_choice` the model calls tools fine.
- `npx playwright test scenarios/mcp-servers-tool-call.spec.ts --list` — compiles/lists.

## Open questions / follow-ups

- Does our LiteLLM version forward `thinking:{"type":"disabled"}`? DeepSeek's docs say
  disabling thinking permits `tool_choice:"required"`, but our proxy still 400s → forced
  tool calls are unavailable on this model today. See `litellm-deepseek-forced-tool-choice`.
- Memory's `isReasonerModel` is a coarse lowercase **substring** list (`deepseek-v4-`,
  `deepseek-reasoner`, `deepseek-r1`, `kvasir`) and skips `tool_choice` for all of them.
  DeepSeek's docs restrict by **mode** (thinking on/off), not by model. It also disables
  DeepSeek thinking when tools are present, which per the docs is what makes `required`
  legal — yet it does not then use `required`. See `memory-deepseek-tool-choice-forcing`.
- `deepseek-v4-flash` is now a **legacy alias** (served by V4.1-Flash); preferred model id
  is `deepseek-flash`. Migration is part of the same memory task.
- CI is blocked org-wide by GitHub Actions billing (`ci-actions-billing-blocker`, existing).
- The test-side signal is now accurate, but it still cannot distinguish "backend never
  offered the tool" from "model declined" without a resolved-tool surface — acceptable
  given `mcp_tool` is the ground truth.

## Tasks

- [litellm-deepseek-forced-tool-choice](../tasks/litellm-deepseek-forced-tool-choice.md) — proxy rejects `required` for DeepSeek thinking models.
- [memory-deepseek-tool-choice-forcing](../tasks/memory-deepseek-tool-choice-forcing.md) — refine `isReasonerModel`; force via thinking-off + required; migrate model id.
- [mcp-servers-tool-call-deploy-verify](../tasks/mcp-servers-tool-call-deploy-verify.md) — marked done (scenario now passes on the deployed release).
