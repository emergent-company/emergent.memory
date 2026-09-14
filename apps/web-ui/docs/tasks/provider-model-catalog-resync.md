# Provider model catalog periodic re-sync

**Status:** done
**Created:** 2026-09-07
**Source:** [../sessions/2026-09-07-litellm-model-catalog.md](../sessions/2026-09-07-litellm-model-catalog.md)

## Done (2026-09-10)

**#404 merged + deployed**: the 6-hourly + startup `ModelCatalogSyncService`
re-resolves configured OpenAI-compatible provider credentials and refreshes
their catalogs (non-fatal throughout).

## Implementation (2026-09-09)

Implemented on **emergent.memory branch `feat/provider-model-catalog-resync`**
(pushed to origin; awaiting review/merge):

- New `ModelCatalogSyncService` (`domain/provider/model_catalog_sync.go`):
  a 6-hourly cron job (`provider:model_catalog:sync`) plus a startup pass
  re-resolve every configured OpenAI-compatible provider credential and refresh
  its `provider_supported_models` snapshot. Non-fatal end to end — decrypt
  failures skip, per-config fetches cap at 15s, and `SyncModels` itself falls
  back to the configured models when `/v1/models` is unreachable — so the job
  cannot take the process down.
- New repo listing `ListProjectProviderConfigsByProvider`
  (`repository.go`, full credential columns; server-internal only).
- fx wiring follows the pricing / model-limits sync pattern
  (`provideModelCatalogSyncService` + `runStartupModelCatalogSync`).

Follow-ups: review/merge/deploy; note that the DB-backed repo + Sync paths are
covered by the POSTGRES-gated suite (no local Postgres in dev), so live
verification happens after deploy.

## What

Add a periodic (or startup) re-sync of `provider_supported_models` for configured
OpenAI-compatible (`openai`/LiteLLM) providers, so newly-added LiteLLM models surface in the
catalog without a manual provider re-save.

## Why

Memory's `SyncModels` only runs on `UpsertProjectConfig`. After PR #377, the catalog is a full
`GET {base_url}/models` snapshot, but it goes stale whenever LiteLLM's model list changes and
no provider config is re-saved. A background job (or startup hook) that re-resolves each
configured OpenAI-compatible provider's credential and calls `SyncModels` would keep the
rates panel + default-model dropdown current.

## Depends on

none

## Notes

- Memory-side only; no gateway change. Lives in `/root/emergent.memory`
  (`apps/server/domain/provider/`).
- Reuse the existing `CredentialService.Resolve` → `ModelCatalogService.SyncModels` path.
- Mind that `SyncModels` for `ProviderOpenAI` is non-fatal on `/v1/models` failure (falls back
  to configured models) — a scheduled job must not fail the process on a transient proxy error.
- Also consider the existing `model_limits_sync.go` (models.dev) job for a scheduler pattern
  to follow.
