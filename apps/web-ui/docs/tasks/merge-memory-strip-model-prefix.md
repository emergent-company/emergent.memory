# Merge memory PR #383 — strip routing prefix from resolved provider model names

**Status:** proposed
**Created:** 2026-09-08
**Source:** [2026-09-08-provider-model-config](../sessions/2026-09-08-provider-model-config.md)

## What

Merge [emergent.memory PR #383](https://github.com/emergent-company/emergent.memory/pull/383),
then re-verify that configuring a **prefixed** provider fallback model works end-to-end in the
gateway (save + "Test provider" button + actual fallback resolution).

## Why

The gateway's provider fallback dropdowns (`f74de1f`) now send prefixed `provider/model` values
for `generativeModel`/`embeddingModel`. Memory's runtime fallback paths already strip the prefix,
but its **resolved credential builders** (`decryptProjectConfig`, `buildTempResolvedCred`) kept it
raw, so the standalone test endpoint and the upsert-time embedding test would send the prefixed
name verbatim to the LLM API and fail. PR #383 centralizes the strip.

## Depends on

- [emergent.memory PR #383](https://github.com/emergent-company/emergent.memory/pull/383) (review/merge)

## Notes

- **PR #383 merged** (`dcc0453c`, 2026-09-08) — the strip is centralized on the
  memory side. Remaining live re-verify (prefixed fallback save + "Test
  provider" + chat fallback) is folded into the provider-config e2e specs and
  is currently gated on dev-env provider/catalog stability
  (`e2e-agent-model-warning-provider-env`).
- After merge + deploy of memory, edit a provider (Settings → Providers → Edit) and confirm:
  - saving with a prefixed embedding model (`google/gemini-embedding-2-preview`) succeeds,
  - the "Test provider" button returns OK (not an API model-not-found error),
  - chat fallback resolves when the project default model is cleared.
- Branch `fix/strip-provider-model-prefix` in the memory repo (clone at
  `/root/alfred/.slim/clonedeps/repos/emergent-company__emergent.memory`); delete after merge.
