## Why

A runaway bulk embedding backfill saturated Postgres I/O (io_wait ~70%, load ~33). The existing `syshealth` monitor plus `ConcurrencyScaler` did not prevent it because (a) the scaler is not applied to the interactive `POST /api/graph/objects/bulk-update` fan-out, (b) the health score stayed "warning" (never critical), and (c) there is no admission control / load shedding on heavy endpoints.

The fix is prevention by demand reduction, not capacity: when the system is saturated, shed heavy work early (HTTP 429 + `Retry-After`) and pause background workers.

## What Changes

- New `pkg/governor` package: a `Governor` that derives a `Normal`/`Throttle`/`Shed` mode from `syshealth.Monitor` with hysteresis, plus non-blocking per-project/global admission control (semaphores + rate limiters) and an echo middleware that returns HTTP 429 with `Retry-After`.
- Config: `GOVERNOR_*` knobs plus `BULK_UPDATE_MAX_ITEMS` (default 100) and `BULK_UPDATE_MAX_DURATION` (default 60s).
- Graph routes: attach the governor's bulk middleware to `bulk-update`, `bulk`, `bulk-upsert`, `bulk-action`, and `reindex-embeddings`.
- Graph service: enforce `BULK_UPDATE_MAX_ITEMS` in `BulkUpdateObjects` (400 when exceeded) and bound the detached context with `BULK_UPDATE_MAX_DURATION` instead of the hardcoded 10m.
- Extraction: the embedding sweep worker skips work while throttled/shed; a `BackgroundPauser` adapter + watcher loop pauses the object/relationship/sweep embedding workers on Shed and resumes them on recovery.
- Prometheus metrics: `governor_mode`, `governor_admission_rejected_total{class,endpoint}`, `governor_admission_inflight{class}`.

## Capabilities

### New Capabilities
- `resource-governance`: admission control and load shedding driven by system health.

### Modified Capabilities
<!-- none -->

## Impact

**Files (`apps/server/`):**
- `pkg/governor/{governor,admission,metrics,module}.go` — new governor package.
- `pkg/governor/{governor,admission}_test.go` — unit tests.
- `internal/config/config.go` — `GovernorConfig` + `BULK_UPDATE_*` graph knobs.
- `domain/graph/routes.go` — bulk middleware.
- `domain/graph/service.go` — bulk-update item cap + duration bound.
- `domain/graph/module.go` — thread config into the service.
- `domain/extraction/embedding_sweep_worker.go` — governor-aware skip.
- `domain/extraction/module.go` — `BackgroundPauser` adapter + watcher lifecycle.
- `cmd/server/main.go` — register `governor.Module`.
- `internal/testutil/server.go` — disabled governor for the test server.

## Non-Goals

- No capacity increase or new worker concurrency.
- No change to the `ConcurrencyScaler` or `syshealth` score calculation.
- No new retention/purge behaviour.
