# 2026-09-07 — Fix LiteLLM model catalog (one-model rates panel)

## Goal

Diagnose why the `openai` provider (LiteLLM proxy) shows only **one** model in the
Alfred Settings → Providers "Rates" panel and default-model dropdown, when LiteLLM
exposes many models. Fix it "as much as possible on memory side".

## Outcome

**Done** — merged to memory as
[PR #377](https://github.com/emergent-company/emergent.memory/pull/377).

Root cause: memory's `ModelCatalogService.SyncModels` stored only the single configured
generative model for OpenAI-compatible providers ("no model catalog API — store the
user-supplied model name directly"). It hit `GET {base_url}/models` only to read that one
model's context window, never to list models. The gateway (`settings_providers.go`
`mergeProviderRates`) merely renders whatever `ListProviderModels` (`GET
/api/v1/providers/{provider}/models`) returns, so a one-row catalog → one rates row.

Fix (memory repo, `apps/server/domain/provider/`): `SyncModels` now fetches the full
`/v1/models` list for `ProviderOpenAI` and caches every model. No gateway change was
needed. After deploy, re-saving the `openai` provider (Settings → Providers → Edit → Save)
triggers `UpsertProjectConfig` → `SyncModels` and repopulates the catalog.

## Decisions

- **Fix on the memory side, not the gateway** — user requested it; the gateway already reads
  the cached catalog via `ListProviderModels`, so a memory-side sync fixes every consumer.
- **Fetch full `/v1/models`, classify by name heuristic** (embedding vs generative), read
  `context_length`/`max_model_len` for limits, strip `vendor/` prefix for display only.
- **Force-include the configured generative + embedding models** (deduped) — never lose the
  user's explicit selection even if the proxy omits it from `/v1/models`.
- **Fall back to the old single-model behavior** when `/v1/models` is unreachable — preserves
  prior semantics on failure.
- **Add model reuse on upsert (`reuseStoredModels`)** — the settings UI sends only
  api_key + base_url; without reuse the now-larger catalog would cause auto-selection to
  clobber the previously configured model on every re-save.
- **Scope to `ProviderOpenAI` only, leave `ProviderDeepSeek` on static list** — DeepSeek's
  static list includes alfred-specific aliases (`deepseek-v4-flash`/`deepseek-v4-pro`) that a
  live `/v1/models` fetch (only `deepseek-chat`/`deepseek-reasoner`) would prune via
  `DeleteSupportedModelsNotIn`.

## Changes

All in `/root/emergent.memory` (memory backend repo; merged, branch deleted):

- `apps/server/domain/provider/catalog.go` — rewrote `SyncModels` into a provider switch with a
  shared upsert+prune tail; added `fetchOpenAICompatibleModels`, `configuredOpenAIModels`,
  `displayNameForOpenAICompatible`; removed the now-dead `queryOpenAICompatibleLimits`.
- `apps/server/domain/provider/service.go` — `UpsertProjectConfig` reuses stored model
  selections when the request omits them; added `reuseStoredModels`.
- `apps/server/domain/provider/catalog_test.go` — 5 new tests (fetch + classification +
  limits, configured-model injection, base_url requirement, fallback dedup, display-name
  stripping).

## Verification

Run from `/root/emergent.memory/apps/server`:

- `go build ./...` — OK
- `go vet ./domain/provider/...` — clean
- `go test ./domain/provider/...` — all pass (incl. 5 new tests)
- `gofmt -l` on the three changed files — clean (after one alignment fix)

## Open questions / follow-ups

- No startup/cron re-sync exists for OpenAI-compatible providers — the catalog refreshes only
  on provider-config upsert. Deferred as a task (see below).
- Remote branch `feat/openai-compatible-model-catalog` could not be deleted (repo rules) —
  left for GitHub/CI to clean up.

## Tasks

- [provider-model-catalog-resync](../tasks/provider-model-catalog-resync.md) — periodic/startup
  re-sync of the OpenAI-compatible model catalog.
