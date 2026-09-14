## Why

Retail pricing (`kb.provider_pricing`) is synced and used to cost LLM usage, but no API exposes it. The gateway needs it to display each model's "automatically set" rate in the companion Settings UI (change `provider-configuration-settings` in the gateway repo).

## What Changes

- Add a read-only `GET /api/v1/pricing` endpoint returning the global retail pricing rows (provider, model, per-modality input + output prices, per 1M tokens).
- Add the corresponding repository method, handler, SDK client method, and (optionally) a CLI listing command.

## Capabilities

### New Capabilities
- `model-pricing-api`: a read-only API exposing per-model retail pricing (input/output prices per 1M tokens).

### Modified Capabilities
<!-- none -->

## Impact

**Files (`apps/server/`):**
- `domain/provider/repository.go` — add `ListPricing(ctx) ([]ProviderPricing, error)`.
- `domain/provider/handler.go` — add `ListPricing` handler.
- `domain/provider/routes.go` — register `GET /api/v1/pricing`.
- `pkg/sdk/provider/client.go` — add `ListPricing` client method.
- `apps/cli/internal/cmd/provider.go` — (optional) `memory provider pricing` list action.

## Non-Goals

- No write path for retail pricing (managed by internal sync only).
- No combined "effective rate" endpoint (the gateway combines retail + overrides client-side).
