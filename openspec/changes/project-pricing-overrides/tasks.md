## 1. Schema

- [x] 1.1 Add migration `00140_create_project_custom_pricing.sql` (goose Up/Down) creating `kb.project_custom_pricing` with `UNIQUE(project_id, provider, model)`, FK to `kb.projects` ON DELETE CASCADE, and index on `project_id`. Verify migration applies via the repo's migration test/runner (see existing migration test patterns).
- [x] 1.2 Add `ProjectCustomPricing` entity to `apps/server/domain/provider/entity.go` (bun tags matching `kb.project_custom_pricing`). Verify `go build ./...`.

## 2. Repository

- [x] 2.1 Add `GetProjectCustomPricing(ctx, projectID, provider, model)`, `ListProjectCustomPricing(ctx, projectID)`, `UpsertProjectCustomPricing(ctx, entry)` (ON CONFLICT `(project_id, provider, model)` DO UPDATE), and `DeleteProjectCustomPricing(ctx, projectID, provider, model)` to `repository.go`. Verify `go build ./...`.
- [x] 2.2 Add unit tests for the four repository methods using the existing DB test harness (insert/list/upsert-replace/delete). Verify `go test ./... -run Pricing -count=1`.

## 3. Optimistic cost resolution

- [x] 3.1 Add a pure `stripVendorModelName(model string) string` helper (take the last `/`-separated path segment, strip a trailing `:tag`/`@version`, lower-case). Reuse the existing `normalizeModelName` (catalog.go) for path-prefix stripping — do NOT redefine `normalizeModelName` (name already taken). Add a unit test table. Verify `go test ./... -run Normalize -count=1`.
- [x] 3.2 Rework `UsageService.calculateCost` to resolve: project override → `GetPricing` exact → `GetPricingByModel` → `GetPricingByModel(normalized)` → $0. Add unit tests covering each layer (exact hit, model-only hit, normalized hit, total miss → 0, project override wins). Verify `go test ./... -run Usage -count=1`.

## 4. Pricing sync provider mapping

- [x] 4.1 Extend `parsePricingEntries` to map `openai`→`ProviderOpenAI` and `deepseek`→`ProviderDeepSeek` (in addition to `google`/`google-vertex`). Add a unit test for the new mappings. Verify `go test ./... -run Pricing -count=1`.

## 5. API endpoints

- [x] 5.1 Add `ListProjectPricingOverrides`, `UpsertProjectPricingOverrides`, and `DeleteProjectPricingOverride` handlers in `handler.go`, enforcing `assertCallerOwnsProject`. Verify `go build ./...`.
- [x] 5.2 Register routes `GET/PUT /api/v1/projects/:projectId/pricing-overrides` and `DELETE /api/v1/projects/:projectId/pricing-overrides/:provider/:model` in `routes.go`. Verify `go build ./...`.
- [x] 5.3 Add handler unit tests: upsert/return, list, delete, 403 cross-project, 404 missing override. Verify `go test ./... -run Override -count=1`.

## 6. SDK + CLI

- [x] 6.1 Add `ListProjectPricingOverrides`, `UpsertProjectPricingOverride`, `DeleteProjectPricingOverride` to `apps/server/pkg/sdk/provider/client.go`. Verify `go build ./...`.
- [x] 6.2 Add a `memory provider pricing` subcommand (list/set/delete) in `apps/cli/internal/cmd/provider.go`. Verify `go build ./...` and a manual `memory provider pricing --help` smoke.

## 7. Verification

- [x] 7.1 Run `go build ./...` and `go vet ./...` from the server module; all pass.
- [x] 7.2 Run `golangci-lint run ./...`; no new issues.
- [x] 7.3 Run the full test suite `go test ./... -count=1`; all pass.
- [ ] 7.4 Manual verification: start the server, upsert a project override for `openai`/`deepseek-v4-pro`, confirm a new usage event for that project carries a non-zero estimated cost, then delete the override and confirm fallback to optimistic global matching.
