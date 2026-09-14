## Context

See proposal.md - Why. Relevant current state in `apps/server/domain/provider/`:

- `LLMUsageEvent` (`entity.go`) carries `ProjectID`, `OrgID`, `Provider`, `Model`, and per-modality token counts. Cost is written at insert time by `UsageService.calculateCost`.
- `calculateCost` (`usage_service.go:107`) resolves `GetOrgCustomPricing` → `GetPricing` (exact `provider + model`) → `$0`.
- Pricing tables (`migrations/00040`): `kb.provider_pricing` (global retail, `UNIQUE(provider, model)`) and `kb.organization_custom_pricing` (org override, `UNIQUE(org_id, provider, model)` — no write path).
- `Repository.GetPricing` (exact) and `GetPricingByModel` (model-only, currently unused) both exist.
- Pricing sync (`pricing_sync.go`) fetches a remote registry (currently 404 → static fallback with DeepSeek prices under `provider="deepseek"`); `parsePricingEntries` only maps `google`/`google-vertex`.
- Provider routes register under `/api/v1` (`routes.go`).

## Goals / Non-Goals

**Goals:**
- Make cost resolution find a price for models served through an OpenAI-compatible/LiteLLM provider (optimistic matching).
- Add a project-scoped manual rate override with a working API surface.

**Non-Goals:**
- No change to how usage events are recorded (the `provider` recorded stays as-is).
- No budget-alert changes; no org-level override UI.
- No data migration of existing org-level rows (the table is unused and left dormant).

## Decisions

1. **Precedence: project override → global retail (optimistic), drop org-level from the path.**
   `calculateCost` becomes: `GetProjectCustomPricing(projectID, provider, model)` → optimistic global resolution → `$0`. Rationale: the user's override requirement is project-scoped; the org table has no write path and no data, so keeping it in the chain adds an unpopulated hop. The org table stays in the DB untouched (harmless); we simply stop consulting it. Alternative considered: keep org as a middle tier — rejected for simplicity and because it is empty.

2. **Optimistic matching via a `resolvePricing` helper, layered:**
   1. Project override exact `(project_id, provider, model)`.
   2. Global exact `(provider, model)` — existing `GetPricing`.
   3. Global model-only — existing `GetPricingByModel(model)`.
   4. Global model-only on a **normalized** model name (strip leading `vendor/` prefix, strip a trailing `:tag`, collapse whitespace, lower-case), then retry `GetPricingByModel`.
   5. `$0` if still no match.
   Rationale: layers 2→4 reuse existing methods; normalization only runs when cheaper lookups miss. Alternative considered: a static `openai→deepseek` alias map — rejected as brittle (new models/vendors would each need a rule).

3. **Normalization is conservative and pure**: reuse the existing `normalizeModelName` (catalog.go — strips `models/` and `publishers/…/models/` prefixes) and add a new `stripVendorModelName` helper (take the last `/` segment, strip a trailing `:tag`/`@version`, lower-case) for vendor-prefixed names like `deepseek/deepseek-v4-pro`. Applied only for the fallback lookup, never persisted, never changes the recorded `model`.

4. **New table `kb.project_custom_pricing`**, mirroring `provider_pricing` columns but scoped by project:
   - `id uuid pk`, `project_id uuid notnull references kb.projects(id) on delete cascade`, `provider varchar(50)`, `model varchar(255)`, `text_input_price`, `image_input_price`, `video_input_price`, `audio_input_price`, `output_price` (all `numeric(12,8) default 0`), `created_at`, `updated_at`.
   - `UNIQUE(project_id, provider, model)`, index on `project_id`.
   - New goose migration (`00140_create_project_custom_pricing.sql`, next available number).

5. **API shape** (project-scoped, mirrors existing usage routes):
   - `GET /api/v1/projects/:projectId/pricing-overrides` → list overrides for the project.
   - `PUT /api/v1/projects/:projectId/pricing-overrides` → upsert one override (body: `{provider, model, textInputPrice, imageInputPrice, videoInputPrice, audioInputPrice, outputPrice}`); upsert on `(project_id, provider, model)`.
   - `DELETE /api/v1/projects/:projectId/pricing-overrides/:provider/:model` → delete.
   All enforce `assertCallerOwnsProject` (same auth as usage routes).

6. **Pricing-sync parser** maps `openai`→`ProviderOpenAI` and `deepseek`→`ProviderDeepSeek` in `parsePricingEntries`, so a working remote registry can carry those providers. Static fallback unchanged.

7. **SDK + CLI**: add read/write client methods to `pkg/sdk/provider/client.go` and a `memory provider pricing` subcommand (list/set/delete) in `apps/cli`. The CLI/SDK are planned for parity but the server API + resolution are the critical path.

## Risks / Trade-offs

- **Model-only matching could over-match an ambiguous model name** → mitigation: exact match is always tried first, and model-only is applied only when it misses; normalization is the last fallback and is conservative.
- **Org override table left dormant** → no data loss (no rows), documented; if it is ever needed, it can be re-wired later.
- **Normalization strips legitimate names** (e.g. a model literally named `vendor/foo`) → mitigation: normalization only affects the fallback price lookup, not storage, and only when earlier lookups already failed.
- **Migration is additive** → rollback is `goose down`; no destructive change.

## Migration Plan

1. Add migration `00140_create_project_custom_pricing.sql` (goose Up/Down).
2. Ship code behind the existing deploy path; no data backfill needed (overrides start empty).
3. Rollback: revert the commit (schema change is additive; `Down` drops the table).

## Open Questions

- Whether to also surface project overrides in the gateway UI — out of scope for this Memory change; the API is the seam.
