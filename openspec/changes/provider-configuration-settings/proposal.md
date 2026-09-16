## Why

Project Settings has no LLM provider configuration. Users cannot see which providers are configured for a project, what models each provider exposes, what each model costs (the automatically-applied retail rate), or override a rate. Provider config and pricing live in Memory; the gateway must surface them so usage cost is transparent and correctable in one place.

## What Changes

- Add a **Providers** section to Project Settings listing every configured provider, its models, and each model's current rate.
- Show each model's rate as **automatic** (Memory's retail pricing) or **custom** (a project override), with the effective rate displayed.
- Let the user override a model's input/output rate (USD per 1M tokens) and remove an override to revert to the automatic rate.
- Overrides write to Memory's project pricing-overrides API (`PUT/DELETE /api/v1/projects/:projectId/pricing-overrides`, already shipped in emergent.memory).

## Capabilities

### New Capabilities
- `provider-settings`: a Project Settings UI that lists configured providers and their models with per-model rates, distinguishing automatic (retail) from custom (overridden) pricing and allowing rate overrides.

### Modified Capabilities
<!-- none -->

## Impact

- **Gateway** (`gateway/`): new Memory client methods (list providers, list models, list retail pricing, list/upsert/delete pricing overrides), a settings handler that aggregates provider/model/rate/override data, and a `Providers` panel in `project_settings.templ` with an override form + routes.
- **Memory dependency**: requires retail pricing to be exposed via API — tracked in the companion change `expose-model-pricing` in `/root/emergent.memory`.

## Non-Goals

- No provider credential editing in the UI (secrets stay env/CLI-driven; the panel is read-only for config, editable only for rates).
- No budget/threshold editing (existing Memory budget-alert behavior is untouched).
