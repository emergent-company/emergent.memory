## Context

See proposal.md - Why. Key facts:

- Memory already exposes: project provider configs (`GET /api/v1/projects/:projectId/providers`), the model catalog (`GET /api/v1/providers/:provider/models`, `GET /api/v1/models`), and project pricing overrides (`GET/PUT/DELETE /api/v1/projects/:projectId/pricing-overrides`, shipped in emergent.memory #369).
- Memory does **not** yet expose retail pricing (`kb.provider_pricing`) — tracked in companion change `expose-model-pricing` (adds `GET /api/v1/pricing`).
- Gateway Settings (`gateway/project_settings.templ`, `gateway/settings.go`) has Project info / Assistant / Agent overrides / Remember & dedup / iOS / Devices / Voice panels — no provider panel.
- Gateway talks to Memory over HTTP via `MemoryClient` (`gateway/memory.go`) using a project-bound `emt_*` token.

## Goals / Non-Goals

**Goals:**
- Surface providers + models + rates in Settings with override capability.

**Non-Goals:**
- No provider credential add/edit/delete in the UI (secrets remain env/CLI-driven).
- No budget/threshold editing.

## Decisions

1. **Read-only provider config, editable rates.** The panel lists providers and models (read-only) and only the rate is editable (override). Rationale: credentials are encrypted/secret and configured via `memory provider configure-*`; exposing them in the gateway is out of scope and a security risk.

2. **Data flow — three Memory reads combined client-side.**
   The settings handler calls:
   1. `ListProjectProviders` → configured providers.
   2. `ListPricing` (new, from `expose-model-pricing`) → retail rates.
   3. `ListProjectPricingOverrides` → overrides.
   It merges into per-model rows: `{provider, model, autoInput, autoOutput, overrideInput?, overrideOutput?, isCustom}`. Rationale: reuses existing + one new endpoint; no combined "effective rate" backend endpoint needed. Alternative: a combined endpoint — rejected as more backend work for marginal gain.

3. **Rate unit and display.** Rates are USD per 1M tokens. The panel shows input and output prices formatted per 1M (e.g. `$1.74 / $3.48`). Automatic rows carry an "auto" badge; overridden rows a "custom" badge plus edit/remove actions.

4. **Override writes to the existing pricing-overrides API.** The override form POSTs to new gateway routes that call `UpsertProjectPricingOverride` / `DeleteProjectPricingOverride` on `MemoryClient`. No new Memory write API needed.

5. **Gateway MemoryClient additions.** Add `ListProjectProviders`, `ListModels` (or reuse `ListAllModels`), `ListPricing`, `ListProjectPricingOverrides`, `UpsertProjectPricingOverride`, `DeleteProjectPricingOverride` methods mirroring existing client patterns in `memory.go`.

6. **UI placement.** Add a `Providers` panel to `project_settings.templ` (new `providersPanel` + `providerModelRow` + `overrideForm`), listed alongside the existing panels, following the existing `ui.Section` + `settingsField` patterns.

## Risks / Trade-offs

- **Memory `expose-model-pricing` is a hard dependency** → the panel's "auto" rate requires `ListPricing`; until that ships, the panel shows "unknown" rate (spec permits). Sequencing: land memory change first, then gateway.
- **Model catalog vs pricing may drift** (catalog has a model, pricing doesn't) → the spec's "no rate available → unknown" scenario covers this; no error.
- **Secrets never leave Memory** → the panel only reads config metadata (provider, model names), never credentials.

## Migration Plan

No schema migration (gateway only). Deploy order: (1) merge + deploy Memory `expose-model-pricing`, (2) merge + deploy gateway `provider-configuration-settings`. Rollback = revert each.

## Open Questions

- Whether to also show per-model estimated spend alongside the rate (reuses the existing usage summary) — deferred; the current scope is rates only.
