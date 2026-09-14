# Strip vendor prefixes in gateway provider-rate panel lookup

**Status:** done
**Created:** 2026-09-09
**Landed:** 2026-09-10 (PR #35)
**Source:** [2026-09-09-embedding-pricing-usage](sessions/2026-09-09-embedding-pricing-usage.md)

## Done (2026-09-10)

Added `stripVendorModelName` in `gateway/settings_providers.go` (mirrors the
memory backend's helper: last `/` segment, drop trailing `:tag`/`@version`,
lower-case + trim) and applied it to the `autoByModel` fallback lookup key
only. The displayed/recorded model name is unchanged. Tests: helper table test
+ a prefixed-catalog (`openai/gemini-embedding-001`) model-only match.
`go build ./...`, `go test ./...`, `golangci-lint run ./...` green.

## What

In `/root/alfred/gateway/settings_providers.go` `mergeProviderRates`, apply a
vendor-prefix strip to the model-only retail-rate fallback (mirroring memory's
`stripVendorModelName` in `emergent.memory/.../domain/provider/usage_service.go`)
so catalog model names that keep a leading `vendor/` prefix (e.g.
`openai/gemini-embedding-001` from a LiteLLM `/models` list) still match a
retail row keyed by bare model name.

## Why

Memory's cost resolution strips prefixes before the model-only fallback; the
gateway panel does an exact model-string match only. Harmless today because
configured embedding/generative models are stored prefix-stripped, but any
prefix-carrying model name in the synced provider catalog renders as "unknown"
rate even when retail pricing exists — a display/cost-drift vs. the backend.

## Depends on
- none (gateway-only change)

## Notes
- Keep the recorded/display model name unmodified; normalize only the lookup
  key, same as the memory backend does.
- Rate rows come from `ListPricing` (memory `kb.provider_pricing`), which needs
  the #405 embedding rows synced for embedding models to resolve at all.
