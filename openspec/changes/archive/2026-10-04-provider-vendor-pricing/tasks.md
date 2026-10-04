# provider-pricing — Tasks

## 1. Feed-sourced retail rates

- [x] Parse the models.dev `cost` block into `ProviderPricing` rows in registry order (deterministic output; skip zero/missing cost).
- [x] Map every registry dialect with a models.dev catalog entry to its provider slug(s); document the unmapped/unpriced vendors and leave them to the $0 fallback.
- [x] Fetch rates over HTTP with a body cap sized for the multi-MB feed and a bounded timeout.

## 2. Sync integration

- [x] Give `PricingSyncService` an injectable `pricingRatesFetcher`; `Sync` overlays fetched rates on the embedded `staticPricing` baseline.
- [x] Fall back to the baseline alone (logged warning, no error) when the feed is unavailable.
- [x] Leave cost-resolution precedence unchanged.

## 3. Tests

- [x] Feed parser: mapped vendor rows, zero/duplicate/unmapped handling, audio-input mapping.
- [x] Sync: baseline-only without a fetcher, remote overlay of a new vendor and an override of a baseline row, fetch-error fallback, upsert-error propagation.
- [x] Resolution: a #1409 vendor resolves non-zero for a listed model; an unlisted model resolves to $0; project override still wins.
- [x] DB-backed `PruneStalePricing`: managed row absent from `keep` is deleted, baseline row present in `keep` survives, non-managed vendor row is untouched (`REQUIRE_DB=1`).

## 4. Verification

- [x] `go build ./...` (apps/server)
- [x] `go test -count=1 ./domain/provider/...`
- [x] `bash apps/server/scripts/lint-ratchet.sh`
- [x] `openspec validate provider-vendor-pricing --strict`
- [x] Parser validated against a real `models.dev/api.json` snapshot (20 mapped vendors, ~1095 sourced rows)

## 5. Review fixes (#1452)

- [x] Presence-aware overlay: parse `input`/`output`/`input_audio` as pointers + a presence set; overwrite only published dimensions so image/audio rates are not zeroed.
- [x] Order vendor slugs by the registry default region (`alibaba-cn` before `alibaba`, `siliconflow-cn` before `siliconflow`); document the base-URL-override caveat.
- [x] Prune managed-vendor rows absent from a successfully parsed feed (never on fetch/parse failure); skip pruning on an empty parsed feed.
- [x] Give `runStartupPricingSync` its own bounded context (cancelled on shutdown) so the baseline upsert survives the short-lived fx `OnStart` context.
- [x] Regression tests: presence-aware overlay (mutation-verified), default-region slug resolution, stale-row prune vs. fetch-failure retention, startup context outlives `OnStart`.
- [x] `openspec validate provider-vendor-pricing --strict`
