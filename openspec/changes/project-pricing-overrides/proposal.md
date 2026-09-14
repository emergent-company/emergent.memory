## Why

LLM usage events are costed at $0 for models served through an OpenAI-compatible (LiteLLM) provider. The project configures `provider=openai` with `deepseek-v4-pro` as the generative model, so usage events record `provider="openai", model="deepseek-v4-pro"` — but pricing for DeepSeek models is keyed under `provider="deepseek"`. Cost resolution does an exact `provider + model` match (`GetPricing`) and returns $0 when it misses. There is also no way to manually override a model's rate: an org-scoped override table exists (`kb.organization_custom_pricing`) but has no write surface (no API/CLI) and is the wrong scope for per-project rate correction.

## What Changes

- **Optimistic cost resolution**: when exact `provider + model` matching fails, fall back to model-only matching, then to a normalized model name, before returning $0. This resolves the OpenAI-compatible/LiteLLM proxy mismatch.
- **Project-level pricing overrides**: add a `kb.project_custom_pricing` table, entity, repository read/write methods, and `GET/PUT/DELETE` API endpoints so a project can manually set per-model rates (prices per 1M tokens).
- **Cost-resolution precedence becomes**: project override → global retail pricing (optimistic). The org-level override table is no longer consulted (left dormant; no data is migrated or dropped).
- **Pricing-sync provider mapping**: extend the remote-registry parser to accept `openai` and `deepseek` provider slugs in addition to `google`/`google-vertex`, so a working remote registry can carry those providers.

## Capabilities

### New Capabilities
- `usage-cost-matching`: optimistic model→price resolution for LLM usage cost estimation (exact → model-only → normalized), replacing the current exact-match-or-zero behavior.
- `project-pricing-overrides`: project-level manual per-model rate overrides — data model, API, and their precedence in cost resolution.

### Modified Capabilities
<!-- none -->

## Impact

**Files changed (`apps/server/domain/provider/`):**
- `entity.go` — add `ProjectCustomPricing` entity (table `kb.project_custom_pricing`).
- `repository.go` — add `GetProjectCustomPricing`, `ListProjectCustomPricing`, `UpsertProjectCustomPricing`, `DeleteProjectCustomPricing`.
- `usage_service.go` — rework `calculateCost` to resolve project override → global retail with optimistic matching.
- `handler.go` — add `ListProjectPricingOverrides`, `UpsertProjectPricingOverrides`, `DeleteProjectPricingOverride`.
- `routes.go` — register `GET/PUT/DELETE /api/v1/projects/:projectId/pricing-overrides`.
- `pricing_sync.go` — extend `parsePricingEntries` provider mapping.
- `apps/server/migrations/` — new migration creating `kb.project_custom_pricing`.

**SDK/CLI:**
- `apps/server/pkg/sdk/provider/client.go` — add pricing-override read/write methods.
- `apps/cli/internal/cmd/provider.go` — add a `memory provider pricing` subcommand (list/set/delete).

**Tests:** unit tests for optimistic matching, project override precedence, and the new endpoints (TDD).
