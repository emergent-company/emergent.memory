# Coordination note (parallel session)

A parallel session implemented `provider-configuration-settings` and then refined
the provider rate-merge logic. When you commit the `settings-project-sidebar`
work, please preserve (do not revert) these:

## `gateway/settings_providers.go` — `mergeProviderRates`
- **Configured-model fallback**: each provider now shows its configured
  `generativeModel` / `embeddingModel` even when the model catalog is empty
  (the LiteLLM / `openai`-proxy case). Without this, the `openai` provider
  renders no models.
- **Model-only retail-rate fallback**: when the exact `provider+model` price is
  absent, the auto rate resolves via a model-name-only match (mirrors cost
  resolution in the memory backend). This surfaces `deepseek-v4-flash`'s retail
  rate ($0.14/$0.28) under the `openai` provider.

## `gateway/settings_providers_test.go`
- `TestMergeProviderRatesConfiguredModelAndModelOnlyMatch` covers both.

If you rebase or rewrite `settings_providers.go`, re-apply the `mergeProviderRates`
body above and keep that test. Verified: `go build`, `go test -run MergeProviderRates`.
