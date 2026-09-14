## 1. Memory client methods

- [x] 1.1 Add `MemoryClient` methods in `gateway/memory.go`: `ListProjectProviders`, `ListModels` (or reuse `ListAllModels`), `ListPricing`, `ListProjectPricingOverrides`, `UpsertProjectPricingOverride`, `DeleteProjectPricingOverride`, with the corresponding response structs (mirror existing client patterns). Verify `go build ./...` from `gateway/`.
- [x] 1.2 Add unit tests for the new client methods using an `httptest` server returning canned JSON (decode + request path/query assertions). Verify `go test ./... -run Provider -count=1`.

## 2. Settings handler

- [x] 2.1 Add a settings handler that loads configured providers + models + retail pricing + pricing overrides and merges them into per-model rows (`provider`, `model`, `autoInput`, `autoOutput`, `overrideInput`, `overrideOutput`, `isCustom`). Verify `go build ./...`.
- [x] 2.2 Add a unit test for the merge logic (override wins, no-override → auto, no-rate → unknown, empty providers). Verify `go test ./... -run Settings -count=1`.

## 3. Providers panel UI

- [x] 3.1 Add a `Providers` panel to `gateway/project_settings.templ` (`providersPanel`, `providerModelRow`, `overrideForm`) listing providers, their models, and each model's rate with auto/custom badges, following the existing `ui.Section`/`settingsField` patterns. Verify `templ generate` + `go build ./...`.
- [x] 3.2 Add a UI test asserting the panel renders providers, models, auto vs custom badges, and the empty state. Verify `go test ./... -run Settings -count=1`.

## 4. Override routes

- [x] 4.1 Add routes + handlers for override set/delete (`POST /settings/providers/:provider/:model`, `POST /settings/providers/:provider/:model/delete`) that call the Memory pricing-overrides API and re-render the panel. Validate numeric, non-negative input. Verify `go build ./...`.
- [x] 4.2 Add unit tests for override set/delete (persist + reflected; invalid input rejected; backend error surfaced). Verify `go test ./... -count=1`.

## 5. Verification

- [x] 5.1 Run `go build ./...` and `go vet ./...` from `gateway/`; all pass.
- [x] 5.2 Run `templ generate` and `task lint`; no new issues.
- [x] 5.3 Run the full test suite `go test ./... -count=1` from `gateway/`; all pass.
- [ ] 5.4 Restart the dev server (`task dev`) and verify in the browser: Settings → Providers shows providers/models/rates, override and remove work, and the changes are reflected after reload.
