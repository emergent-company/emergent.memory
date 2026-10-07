## Why

The resource governor sheds `ClassBulk` work on **host-global** I/O wait / CPU load. A bulk migration raises host I/O itself, so it is shed forever — a livelock: the very work that triggers the shed is the work being shed, and it never makes progress.

The fix is not a bypass, it is a correctness fix: `Shed` must be driven only by **attributable capacity** — the resources a bulk operation actually consumes and can be held accountable for (database pool utilization and in-flight bulk admissions). Host I/O/CPU remain a `Throttle` signal (bounded backpressure), never a shed trigger.

## What Changes

- `Shed` is now driven only by attributable capacity: DB pool utilization `>= ShedDBPoolPercent` **AND** in-flight bulk admissions `>= ShedBulkInflightBound`. Host I/O/CPU drive `Throttle` only.
- `ClassBulk` under `Throttle` uses bounded backpressure: wait up to `BulkWaitBudget` (~2s) for capacity, then HTTP 429 + `Retry-After`. Bulk is never shed in `Throttle`.
- Bulk work is exempt from the per-project rate limiter (governed by inflight + backpressure); interactive keeps its rate limit.
- Class-segregated global budgets (interactive vs bulk) so a bulk op cannot 429 interactive work.
- Background workers pause on `Throttle` and `Shed`, resume on `Normal`.
- `ShedBulkInflightBound=1`; split globals = 1 interactive / 1 bulk.
- Fix the dead `DBPoolPercent` metric: `syshealth.NewMonitor` reads pool stats from `*pgxpool.Pool` directly (the bun `stdlib.OpenDBFromPool` wrapper never sets `MaxOpenConns`, so `DBPoolPercent` was always 0).
- Surface governor mode / DB pool % / bulk inflight as informational fields on `/health`.

## Capabilities

### Modified Capabilities
- `resource-governance`: shed on attributable capacity, bounded backpressure bulk admission, class-segregated budgets, and governor health surfacing.

## Impact

**Files (`apps/server/`):**
- `pkg/syshealth/monitor.go` — `NewMonitor(pool)` + `poolStats` override; fix `DBPoolPercent`.
- `pkg/syshealth/monitor_test.go` — pool stats override tests.
- `pkg/governor/{governor,admission,module}.go` — attributable-capacity shed, `AdmitWait`, class-segregated budgets, health snapshot.
- `pkg/governor/{governor,admission}_test.go` — updated/new admission tests.
- `internal/config/config.go` — `GovernorConfig` fields.
- `domain/extraction/module.go` — inject `*pgxpool.Pool` for the monitor.
- `domain/health/handler.go` — `GovernorStatus` in the health response.
- `domain/graph/routes.go` — gate relationships `/bulk` + `/bulk-upsert`; keep `bulk-update-status` ungated.

## Non-Goals

- No bypass or exemption for bulk migrations — the governor is corrected, not disabled.
- No change to the `ConcurrencyScaler` or `syshealth` score calculation.
- No capacity increase or new worker concurrency.
