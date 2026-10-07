## 1. System health: fix dead DBPoolPercent

- [x] 1.1 `pkg/syshealth/monitor.go`: `NewMonitor(cfg, pool *pgxpool.Pool, log)`; add overridable `poolStats func() (inUse, max int32)`; `collect()` reads pool stats (0 max ⇒ 0, never NaN).
- [x] 1.2 `domain/extraction/module.go`: `provideSysHealthMonitor` injects `*pgxpool.Pool` instead of `bun.IDB`.
- [x] 1.3 `pkg/syshealth/monitor_test.go`: `poolStats` override (9/10 ⇒ 90; 0/0 ⇒ 0).

## 2. Governor: attributable-capacity shed + backpressure

- [x] 2.1 `pkg/governor/governor.go`: remove `ShedIOWaitPercent`/`ShedCPULoadFactor`/`MaxInflightGlobal`; add `ShedDBPoolPercent`, `ShedBulkInflightBound`, `MaxInflightGlobalInteractive`, `MaxInflightGlobalBulk`, `RatePerMinutePerProjectBulk`, `BulkWaitBudget`, `MaxWaiters`.
- [x] 2.2 Split `globalSem` into `globalInteractiveSem`/`globalBulkSem`; add `waiterSem` + `bulkInflight`.
- [x] 2.3 `Mode()`: shed only on DB pool ≥ threshold AND bulk inflight ≥ bound; host I/O/CPU throttle only.
- [x] 2.4 `Admit()`: class-segregated globals; interactive rate limiter only.
- [x] 2.5 `AdmitWait()`: bounded backpressure bulk admission (waiter bound, blocking global + per-project, optional bulk rate, in-flight tracking, idempotent release).
- [x] 2.6 `Snapshot()` + `BulkInflight()`.
- [x] 2.7 `RunModeWatcher`: pause on Throttle/Shed, resume on Normal.

## 3. Admission middleware

- [x] 3.1 `pkg/governor/admission.go`: bulk uses `AdmitWait`; interactive uses `Admit`; log bulk/interactive inflight split on reject.

## 4. Config

- [x] 4.1 `internal/config/config.go`: new `GovernorConfig` env fields; remove shed/global-host fields.
- [x] 4.2 `pkg/governor/module.go`: map all fields; `BulkWaitBudget` from ms config.

## 5. Health surfacing

- [x] 5.1 `domain/health/handler.go`: `gov *governor.Governor` + `GovernorStatus` field (informational only).

## 6. Graph routes

- [x] 6.1 `domain/graph/routes.go`: gate relationships `/bulk` + `/bulk-upsert`; comment the deliberate `bulk-update-status` exemption.

## 7. Verification

- [x] 7.1 `go build ./...` from `apps/server`.
- [x] 7.2 `go test ./pkg/governor/... ./pkg/syshealth/... ./domain/health/... ./domain/graph/... ./internal/config/...`.
- [x] 7.3 `go vet ./pkg/governor/... ./pkg/syshealth/...`.
- [x] 7.4 `gofmt -l` clean on changed files.
