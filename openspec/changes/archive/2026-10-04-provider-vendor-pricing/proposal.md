## Why

`kb.provider_pricing` — populated only from the embedded `staticPricing` list — covers just the four legacy vendors (`google`, `google-vertex`, `openai`, `deepseek`). The 24 vendors added by the data-driven registry (#1409) have no rows, so their usage events and cost views resolve at the $0 fallback and report unknown rates.

The repo already integrates a pricing-capable catalog: `models.dev/api.json` is fetched daily for model token limits (`model_limits_sync.go`), and its per-model `cost` block carries provider-published USD-per-1M-token rates. #1409 explicitly deferred "per-vendor native pricing ingestion beyond static rows"; this change completes it.

## What Changes

- Add a models.dev rate sync: fetch the feed, parse each model's `cost` block for registered vendors, and overlay those rows onto the embedded baseline.
- Map registry dialects to models.dev provider slugs (`pricing_modelsdev.go`). Vendors with no models.dev entry, or whose entries are unpriced, are documented and fall through to the existing $0 default rather than being assigned invented rates.
- Keep `staticPricing` as the always-upserted baseline/fallback; a feed outage must not remove known rates.
- Leave cost-resolution precedence unchanged (project override → exact `(provider, model)` → model-only → normalized → $0).
- No DB migration: rows are code-seeded, exactly as before.

## Capabilities

### New Capabilities
- `provider-pricing`: the retail-rate catalog for LLM/embedding vendors — sourced from models.dev, overlaid on an embedded baseline, with transparent handling of unpriced vendors.

### Modified Capabilities
<!-- None. Cost resolution semantics are unchanged. -->

## Impact

**Server (`apps/server/`)**
- `domain/provider/pricing_modelsdev.go` (new): models.dev cost parsing, vendor→slug map, HTTP fetcher.
- `domain/provider/pricing_sync.go`: `PricingSyncService` gains a `pricingRatesFetcher`; `Sync` merges fetched rates over `staticPricing` with fallback on fetch failure.
- `domain/provider/pricing_sync_test.go`, `pricing_resolution_test.go`: feed parser, merge/fallback, and vendor-resolution tests.

**Data**
- No schema change. `kb.provider_pricing` is upserted from code as before; the daily sync simply writes more rows.
