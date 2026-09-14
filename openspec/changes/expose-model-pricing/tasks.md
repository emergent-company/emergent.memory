## 1. Repository

- [x] 1.1 Add `ListPricing(ctx context.Context) ([]ProviderPricing, error)` to `apps/server/domain/provider/repository.go` — `SELECT` all `provider_pricing` rows ordered by `provider, model`, returning an empty slice (not nil) on no rows. Verify `go build ./...` from `apps/server/`.
- [x] 1.2 Add a DB-backed unit test for `ListPricing` (empty, populated, ordering) using the `internal/testutil` harness. Verify `go test ./... -run Pricing -count=1`.

## 2. Handler + route

- [x] 2.1 Add `ListPricing` handler to `apps/server/domain/provider/handler.go` returning `c.JSON(http.StatusOK, rows)`. Verify `go build ./...`.
- [x] 2.2 Register `api.GET("/pricing", h.ListPricing)` in `apps/server/domain/provider/routes.go` and update the route-group comment. Verify `go build ./...`.
- [x] 2.3 Add a handler unit test (200 with rows, 200 empty). Verify `go test ./... -run Pricing -count=1`.

## 3. SDK + CLI

- [x] 3.1 Add a `ProviderPricing` type (json tags matching the server's PascalCase keys) and a `ListPricing(ctx) ([]ProviderPricing, error)` method to `apps/server/pkg/sdk/provider/client.go`. Verify `go build ./...` from `apps/server/pkg/sdk`.
- [x] 3.2 Add a unit test for the SDK `ListPricing` method using a mock server. Verify `go test ./... -run Pricing -count=1` from `apps/server/pkg/sdk`.
- [x] 3.3 (Optional) Add a `memory provider pricing list` action to `apps/cli/internal/cmd/provider.go`. Verify `go build ./...` from `apps/cli`.

## 4. Verification

- [x] 4.1 Run `go build ./...` and `go vet ./...` from `apps/server/`; all pass.
- [x] 4.2 Run `golangci-lint run ./...` from `apps/server/`; no new issues.
- [x] 4.3 Run the full test suite `go test ./... -count=1` from `apps/server/`; all pass.
- [ ] 4.4 Manual: `GET /api/v1/pricing` returns retail pricing rows after deploy.
