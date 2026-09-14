# Deploy + verify embedding pricing and LiteLLM embedding usage

**Status:** proposed
**Created:** 2026-09-09
**Source:** [2026-09-09-embedding-pricing-usage](sessions/2026-09-09-embedding-pricing-usage.md)

## What

Roll the Memory backend release containing PR #405 (embedding retail prices)
and PR #408 (OpenAI-compatible/LiteLLM embedding usage events) out to the
running Memory deployment, then verify the live surfaces:

- Pricing sync runs (server startup or 02:00 UTC cron) and
  `kb.provider_pricing` gains rows for `gemini-embedding-001`, etc.
- Providers rate panel (gateway) shows an auto rate for
  `openai/gemini-embedding-001` / the configured embedding model instead of
  "unknown".
- Embedding work through the LiteLLM/`openai` provider emits
  `llm_usage_events` (`operation=embed`, provider `openai`) and budget/usage
  pages show embedding spend.

## Why

Both fixes are merged to `main` but have no effect until the running server
binary + pricing table catch up. The user's reported symptom ("rates for this
model cannot be found") is only resolved once the deployment is live.

## Depends on
- Memory PR #405 and #408 merged (done — `2e354db8`, `fa1cf2ab`).

## Notes
- Hot reload/`air` alone will not apply these changes cleanly (compile-time
  data + new methods); restart the server.
- Embedding usage for OpenAI-compatible providers requires the embedding call
  to go through `pkg/embeddings` `Embed*WithUsage` (graph/chunk/sweep workers
  do; plain `EmbedQuery` paths are not usage-tracked by design).
