# Restore or retire the model-pricing registry

**Status:** done
**Created:** 2026-09-09
**Landed:** 2026-09-10 (emergent.memory PR #413)
**Source:** [2026-09-09-embedding-pricing-usage](sessions/2026-09-09-embedding-pricing-usage.md)

## Done (2026-09-10) — option 2: static list is canonical

Confirmed `emergent-company/model-pricing` does not exist (404 for everyone), so
option 1 was impossible. Chose option 2: the embedded `staticPricing` list is the
canonical source.

`apps/server/domain/provider/pricing_sync.go`: `Sync` now upserts
`staticPricingEntries()` directly (no network); removed `fetchRemotePricing`,
`pricingEntry`, `pricingFetchURL`, `pricingFetchTimeout`, the HTTP client, and
unused imports. The daily cron + startup sync are kept (they seed/refresh the
table from the static list). Doc comments updated to stop claiming a remote
registry is authoritative. Tests updated to assert a hermetic static upsert.
Embedding rows from #405 were already present. Build / vet / unit tests / lint
green (PR #413).

## What

The retail pricing registry Memory syncs from —
`https://raw.githubusercontent.com/emergent-company/model-pricing/main/pricing.json`
(`pricingFetchURL` in
`emergent.memory/apps/server/domain/provider/pricing_sync.go`) — returns 404 for
everyone (repo does not exist publicly / is private). `PricingSyncService` logs
a warning every sync and always falls back to the embedded `staticPricing`
list.

Options:
1. Restore/publish the `emergent-company/model-pricing` repo (embedding rows
   must be added there too, matching what #405 put in the static list), or
2. Drop the remote fetch path entirely and treat `staticPricing` as canonical
   (remove the daily-cron sync + startup fetch noise), or
3. Leave as-is and document that the remote path is dead.

## Why

The remote-fetch machinery is dead weight and the failure path hides the real
source of truth (the embedded list). Any future price edit must touch the
static list; a developer following the code could wrongly conclude the registry
is authoritative and push only there.

## Depends on
- Memory PR #405 (embedding rows in `staticPricing`) — merged.

## Notes
- `UpsertPricing` only upserts; it never deletes stale rows, so reverting the
  remote path is safe for existing DBs.
- If the registry is revived, remember the parse step only accepts providers
  `google` / `google-vertex` / `openai` / `deepseek`.
