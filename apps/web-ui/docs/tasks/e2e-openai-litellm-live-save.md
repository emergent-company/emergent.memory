# E2E: reach a live OpenAI-compatible provider from dev memory

**Status:** done
**Created:** 2026-09-09
**Source:** [2026-09-09-e2e-litellm-provider](../sessions/2026-09-09-e2e-litellm-provider.md)

## What

Make the openai-provider save in the e2e suite complete against dev memory
(`api.dev.emergent-company.ai`): configure a LiteLLM proxy that the memory
backend can reach so its catalog sync (`GET {base_url}/models`) succeeds and
the live generate test passes.

## Why

`scenarios/provider-openai-litellm-config.spec.ts` exercises every UI step
green, but the final save is rejected by dev memory with `no models in catalog
for provider openai (sync models before testing)` even when the submitted
base URL + key are reachable and valid from the dev box. The scenario therefore
env-gates and skips (by design), but the full success path — save redirect,
provider row with the LiteLLM base URL, Edit link — is never asserted against
real infra.

## Depends on

- none (infra/wiring, not code)
- related: [provider-model-catalog-resync](provider-model-catalog-resync.md)

## Notes

- Verified the LiteLLM proxy answers `GET /v1/models` (HTTP 200) with the dev
  key from the gateway box; the failure is api.dev-side reachability/catalog
  sync, not credential validity.
- Candidates: run the e2e gateway/memory stack against a LiteLLM instance on
  the same network (tailnet or compose), or expose a reachable proxy URL via
  `E2E_OPENAI_BASE_URL`.

## Resolution

Done 2026-09-10. The block was two emergent.memory bugs, not infra wiring:
**#399** (catalog-upsert SQL `SQLSTATE 42P01` via a bad qualified reference) and
**#400** (openai-compatible rows persisted with an empty `provider`). Both merged
and deployed to api.dev (`v0.75.0`/`v0.76.0`), after which the openai/LiteLLM
provider save completes live — the provider-config scenario passes and the
full-journey `blueprint-object-chat` scenario saves the provider too. See
[2026-09-10-scenario-chat-journey](../sessions/2026-09-10-scenario-chat-journey.md).
