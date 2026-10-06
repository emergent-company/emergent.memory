## 1. Governor package

- [x] 1.1 `pkg/governor/governor.go`: `Mode`/`Class` types, `Config`, `Governor`, `New`, `Mode()` (hysteresis, timestamp-gated, stale-safe), `Admit` (non-blocking, idempotent release), sentinel `ErrShed`/`ErrNoCapacity`, `BackgroundPauser` interface, `RunModeWatcher`.
- [x] 1.2 `pkg/governor/admission.go`: `Middleware(class)` returning 429 + `Retry-After` with an apperror-shaped body.
- [x] 1.3 `pkg/governor/metrics.go`: `governor_mode`, `governor_admission_rejected_total{class,endpoint}`, `governor_admission_inflight{class}`.
- [x] 1.4 `pkg/governor/module.go`: fx `Module` + `ProvideGovernor` mapping app config to governor config.
- [x] 1.5 Unit tests (`governor_test.go`, `admission_test.go`): safe→normal; throttle→shed hysteresis; shed rejects bulk but not interactive; per-project no-capacity; idempotent release; 429 + Retry-After; timestamp gating; stale health never sheds.

## 2. Config

- [x] 2.1 Add `GovernorConfig` (GOVERNOR_* env tags) and `Governor` field to `Config`.
- [x] 2.2 Add `BULK_UPDATE_MAX_ITEMS` (default 100) and `BULK_UPDATE_MAX_DURATION` (default 60s) to `GraphConfig`.

## 3. Graph domain

- [x] 3.1 `routes.go`: attach `governor.Middleware(ClassBulk)` to `bulk-update`, `bulk`, `bulk-upsert`, `bulk-action`, `reindex-embeddings`; thread governor into `RegisterRoutes`.
- [x] 3.2 `service.go`: replace hardcoded 10m const with `bulkUpdateMaxDuration` (configured); enforce `bulkUpdateMaxItems` in `BulkUpdateObjects` (400 when exceeded).
- [x] 3.3 `module.go`: provide service with config-driven bulk limits via `provideService`.

## 4. Extraction domain

- [x] 4.1 `embedding_sweep_worker.go`: skip sweep when `Mode()` is Throttle or Shed.
- [x] 4.2 `module.go`: `provideBackgroundPauser` (wraps `EmbeddingControlHandler`) + `RegisterGovernorModeWatcherLifecycle` (pause on Shed, resume on recovery).

## 5. Wiring

- [x] 5.1 `cmd/server/main.go`: register `governor.Module`.
- [x] 5.2 `internal/testutil/server.go`: pass a disabled governor to `graph.RegisterRoutes`.

## 6. Verification

- [x] 6.1 `go build ./...` from `apps/server`.
- [x] 6.2 `go test ./pkg/governor/... ./domain/graph/... ./domain/extraction/... ./internal/config/...`.
- [x] 6.3 `golangci-lint run` on the governor package and changed files (0 new issues).
- [x] 6.4 `gofmt -l` clean on all changed files.
