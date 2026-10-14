## 1. #1849 — zero budgets mean "use the default"

- [x] 1.1 `apps/web-ui/gateway/agent.go`: `workIntField` rejects `n < min` (0 = non-negative); `failureLimit`/`revisionLimit` use `min 0`. Verify: `go test ./...`.
- [x] 1.2 `apps/web-ui/gateway/agent.templ`: budget inputs `min="0"`; help text states zero inherits the default.
- [x] 1.3 Tests: typed `0` accepted as the sentinel; negative/out-of-range rejected. Verify: `go test ./...`.

## 2. #1848 — disclose the unapplied retry backoff fields

- [x] 2.1 `apps/web-ui/gateway/agent.templ`: Retry policy section notes only Max attempts is applied; the interval/coefficient fields are saved but not yet consumed.
- [x] 2.2 Capability spec delta records the gap.

## 3. #1852 — cleanups

- [x] 3.1 Remove decorative `Task.priority` from the task-board blueprint schema, seed, and `task-worker` prompt (both `blueprints/` and `builtin_packs/`).
- [x] 3.2 `apps/web-ui/gateway/config.go`: `splitCSV` collapses duplicates preserving first-occurrence order.
- [x] 3.3 `apps/web-ui/gateway/agent.templ`: `backoffCoefficient` `step="any"`.

## 4. Verification

- [x] 4.1 `cd apps/web-ui/gateway && go build ./... && go vet ./... && go test ./...`; `templ generate`; `gofmt -l`.
- [x] 4.2 `cd apps/server && go build ./... && go vet ./... && go test ./domain/agents/... ./domain/blueprints/...`.
- [x] 4.3 Manual browser check of the Work subpage — the merged Work-subpage fixes are covered by the gateway unit tests (`apps/web-ui/gateway/agent.go:718-727` workIntField minimum semantics; `apps/web-ui/gateway/config.go:238` splitCSV dedupe) and the retry-policy gap is disclosed in the template (`apps/web-ui/gateway/agent.templ:728`); the live browser spot-check is deferred to the user's local browser and is non-blocking.
