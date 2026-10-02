# Tasks — add-budget-page-and-alert-config

## 1. Server: expose and validate the threshold

- [x] 1.1 Add `BudgetAlertThreshold *float64` to `ProjectDTO` and populate it in `Project.ToDTO()` so the read path always exposes the effective fraction (`apps/server/domain/projects/entity.go`); verify `TestToDTOIncludesBudgetAlertThreshold`
- [x] 1.2 Add `validateBudgetAlertThreshold` (reject `<= 0` or `> 1`, `apperror` 400 `validation-failed`) and call it from `Service.Update` before persisting `req.BudgetAlertThreshold` (`apps/server/domain/projects/service.go`); verify `TestValidateBudgetAlertThreshold` covers 0, negative, `>1` (rejected) and positive fractions including 1.0 (accepted)
- [x] 1.3 Verify `cd apps/server && go build ./...` and `go test -count=1 ./domain/projects/... ./domain/provider/...` (keeps #1347 budget-alert tests green)

## 2. Gateway: the Budget page

- [x] 2.1 Add `BudgetAlertThreshold` to the gateway `Project` read model (`apps/web-ui/gateway/settings.go`)
- [x] 2.2 Add `budgetSettingsPageData` and `uiProjectBudgetSettings` (GET `/settings/budget`), mirroring the Devices settings handler; register the route in `main.go`
- [x] 2.3 Add the `budget_alert_threshold` case to the inline field-save handler: parse a percentage, reject empty / non-numeric / `<= 0` / `> 100`, store `pct/100` as the fraction (`apps/web-ui/gateway/settings_handlers.go`); verify `TestUIProjectSettingsProjectFieldBudgetThreshold`
- [x] 2.4 Move the Budget (USD) fieldset out of `projectInfoPanel` into a new `BudgetSettingsPage`/`budgetPanel`, add the `budget` settings-rail entry, and add `thresholdPercentInputValue` so the fraction renders as a percentage (`apps/web-ui/gateway/project_settings.templ`); verify `TestRenderBudgetSettingsPage` and the updated `TestRenderProjectSettingsPage` / `TestRenderSettingsSubNav`
- [x] 2.5 Add the gateway route test `TestUIProjectBudgetSettingsRoute` and update the client read/write tests (`settings_test.go`, `settings_handlers_test.go`)

## 3. Verification

- [x] 3.1 `cd apps/server && PATH="/root/go/bin:$PATH" go build ./... && go test -count=1 ./domain/provider/... ./domain/projects/...`
- [x] 3.2 `cd apps/web-ui/gateway && PATH="/root/go/bin:$PATH" templ generate && go build ./... && go test ./...`
- [x] 3.3 `node --check` on changed JS (no JS changed) and the hermetic js-dom gate as CI runs it
- [x] 3.4 `golangci-lint run ./...` and `bash apps/server/scripts/lint-ratchet.sh`
- [x] 3.5 `PATH="/root/go/bin:$PATH" openspec validate add-budget-page-and-alert-config --strict`
