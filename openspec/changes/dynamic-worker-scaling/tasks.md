# Dynamic Worker Scaling - Implementation Tasks

## 1. Setup and Dependencies

- [x] 1.1 Add `github.com/shirou/gopsutil/v3` dependency to `apps/server-go/go.mod`
- [x] 1.2 Create `apps/server-go/pkg/syshealth/` package directory structure
- [x] 1.3 Run `go mod tidy` to download dependencies

## 2. Health Monitor Core Implementation

- [x] 2.1 Implement `pkg/syshealth/config.go` with configuration structs (collection interval, thresholds)
- [x] 2.2 Implement `pkg/syshealth/types.go` with HealthMetrics, HealthZone enum, and Monitor interface
- [x] 2.3 Implement `pkg/syshealth/monitor.go` - Monitor struct with Start/Stop/GetHealth methods
- [x] 2.4 Implement CPU load average collection using `gopsutil/v3/load`
- [x] 2.5 Implement I/O wait percentage collection using `gopsutil/v3/cpu`
- [x] 2.6 Implement memory utilization collection using `gopsutil/v3/mem`
- [x] 2.7 Implement database connection pool metrics collection using Bun's `db.Stats()`
- [x] 2.8 Implement weighted health score calculation (I/O 40%, CPU 30%, DB 20%, Mem 10%)
- [x] 2.9 Implement health zone determination (critical 0-33, warning 34-66, safe 67-100)
- [x] 2.10 Add graceful degradation for metric collection failures (use last known values)
- [x] 2.11 Add staleness tracking (mark metrics stale if >2 minutes old)

## 3. Health Monitor Logging

- [x] 3.1 Add periodic health logging at INFO level (every 30s with health score and key metrics)
- [x] 3.2 Add health zone transition logging at WARN level
- [x] 3.3 Add metric collection failure logging at ERROR level
- [x] 3.4 Add persistent collection failure logging at CRITICAL level (3+ consecutive failures)

## 4. Concurrency Scaler Implementation

- [x] 4.1 Implement `pkg/syshealth/scaler.go` - ConcurrencyScaler struct
- [x] 4.2 Implement `GetConcurrency()` method with health-based adjustment logic
- [x] 4.3 Implement three-zone scaling rules (critical→min, warning→50%, safe→max)
- [x] 4.4 Implement gradual scaling with 50% max increase per cycle
- [x] 4.5 Implement cooldown period tracking (5 min for increase, 1 min for decrease)
- [x] 4.6 Implement cooldown bypass for critical health zone
- [x] 4.7 Implement concurrency bounds validation (min ≥ 1, max ≥ min, max ≤ 50)
- [x] 4.8 Implement stale health data handling (default to score 50, 50% of max concurrency)

## 5. Worker Configuration Extension

- [x] 5.1 Extend `GraphEmbeddingConfig` struct with `EnableAdaptiveScaling`, `MinConcurrency`, `MaxConcurrency` fields
- [x] 5.2 Extend `ChunkEmbeddingConfig` struct with adaptive scaling fields
- [x] 5.3 Extend `DocumentParsingConfig` struct with adaptive scaling fields
- [x] 5.4 Extend `ObjectExtractionConfig` struct with adaptive scaling fields
- [x] 5.5 Add default values (enable=false, min=1, max=10) to all worker config constructors

## 6. Worker Integration

- [x] 6.1 Add ConcurrencyScaler dependency to GraphEmbeddingWorker struct
- [x] 6.2 Update GraphEmbeddingWorker constructor to inject scaler
- [x] 6.3 Replace static concurrency with `w.scaler.GetConcurrency(w.cfg.WorkerConcurrency)` in processBatch (line ~188)
- [x] 6.4 Add ConcurrencyScaler dependency to ChunkEmbeddingWorker struct
- [x] 6.5 Update ChunkEmbeddingWorker constructor to inject scaler
- [x] 6.6 Replace static concurrency with scaler call in ChunkEmbeddingWorker processBatch
- [x] 6.7 Add ConcurrencyScaler dependency to DocumentParsingWorker struct
- [x] 6.8 Update DocumentParsingWorker constructor to inject scaler
- [x] 6.9 Replace static concurrency with scaler call in DocumentParsingWorker processBatch
- [x] 6.10 Add ConcurrencyScaler dependency to ObjectExtractionWorker struct
- [x] 6.11 Update ObjectExtractionWorker constructor to inject scaler
- [x] 6.12 Replace static concurrency with scaler call in ObjectExtractionWorker processBatch

## 7. Dependency Injection (FX Module)

- [x] 7.1 Update `domain/extraction/module.go` to provide Monitor as singleton
- [x] 7.2 Add Monitor initialization with config (30s interval, default thresholds)
- [x] 7.3 Add Monitor lifecycle hooks (start on app start, stop on shutdown)
- [x] 7.4 Update module to provide ConcurrencyScaler for each worker type
- [x] 7.5 Wire up Monitor to all ConcurrencyScaler instances

## 8. Configuration API Extension

- [x] 8.1 Extend embedding config request DTO with `EnableAdaptiveScaling`, `MinConcurrency`, `MaxConcurrency` fields
- [x] 8.2 Extend embedding config response DTO with adaptive scaling fields + `CurrentConcurrency`, `HealthScore`
- [x] 8.3 Update `POST /admin/extraction/embedding/config` handler to accept and validate new fields
- [x] 8.4 Add validation: min ≥ 1, max ≥ min, max ≤ 50
- [x] 8.5 Update config persistence to database (store new fields)
- [x] 8.6 Update `GET /admin/extraction/embedding/config` handler to include current runtime values
- [x] 8.7 Add backward compatibility handling (worker_concurrency maps to max_concurrency if adaptive enabled)
- [x] 8.8 Add configuration change logging with operator identity

## 9. Prometheus Metrics

- [ ] 9.1 Implement `pkg/syshealth/metrics.go` for Prometheus metrics registration
- [ ] 9.2 Add `system_health_score` gauge with zone label
- [ ] 9.3 Add `system_io_wait_percent` gauge
- [ ] 9.4 Add `system_cpu_load_avg` gauge with period label (1m, 5m, 15m)
- [ ] 9.5 Add `system_memory_utilization_percent` gauge
- [ ] 9.6 Add `system_db_pool_utilization_percent` gauge
- [ ] 9.7 Add `extraction_worker_current_concurrency` gauge with worker_type label
- [ ] 9.8 Add `extraction_worker_concurrency_adjustments_total` counter with worker_type, direction, reason labels
- [ ] 9.9 Add `extraction_jobs_throttled_total` counter with worker_type label
- [ ] 9.10 Update Monitor to publish metrics on each collection cycle
- [ ] 9.11 Update ConcurrencyScaler to publish metrics on concurrency changes

## 10. Unit Tests - Health Monitor

- [ ] 10.1 Create `pkg/syshealth/monitor_test.go`
- [ ] 10.2 Test metric collection with mocked gopsutil functions
- [ ] 10.3 Test health score calculation with various metric combinations
- [ ] 10.4 Test health zone determination (critical, warning, safe)
- [ ] 10.5 Test weighted scoring formula (40% I/O, 30% CPU, 20% DB, 10% Mem)
- [ ] 10.6 Test graceful degradation on metric collection failures
- [ ] 10.7 Test staleness tracking (metrics older than 2 minutes)
- [ ] 10.8 Test Start/Stop lifecycle
- [ ] 10.9 Test concurrent GetHealth calls (RWMutex correctness)

## 11. Unit Tests - Concurrency Scaler

- [ ] 11.1 Create `pkg/syshealth/scaler_test.go`
- [ ] 11.2 Test GetConcurrency with adaptive scaling disabled (returns static value)
- [ ] 11.3 Test GetConcurrency in critical health zone (returns min concurrency)
- [ ] 11.4 Test GetConcurrency in warning health zone (returns 50% of max)
- [ ] 11.5 Test GetConcurrency in safe health zone (returns max concurrency)
- [ ] 11.6 Test gradual scaling (max 50% increase per cycle)
- [ ] 11.7 Test cooldown period enforcement (5 min for increase, 1 min for decrease)
- [ ] 11.8 Test cooldown bypass in critical health zone
- [ ] 11.9 Test concurrency bounds enforcement (min, max)
- [ ] 11.10 Test stale health data handling (defaults to 50% of max)

## 12. Unit Tests - Worker Integration

- [ ] 12.1 Update `graph_embedding_worker_test.go` to test adaptive scaling integration
- [ ] 12.2 Test GraphEmbeddingWorker with mock scaler (verify GetConcurrency is called)
- [ ] 12.3 Test ChunkEmbeddingWorker with mock scaler
- [ ] 12.4 Test DocumentParsingWorker with mock scaler
- [ ] 12.5 Test ObjectExtractionWorker with mock scaler
- [ ] 12.6 Test worker behavior when scaler returns different concurrency values

## 13. Integration Tests

- [ ] 13.1 Create `pkg/syshealth/integration_test.go`
- [ ] 13.2 Test end-to-end: Monitor → Scaler → Worker concurrency adjustment
- [ ] 13.3 Test simulated health degradation (inject high I/O wait, verify concurrency reduces)
- [ ] 13.4 Test simulated health recovery (inject safe metrics, verify gradual concurrency increase)
- [ ] 13.5 Test multiple workers with shared Monitor (verify all respond to health changes)
- [ ] 13.6 Test configuration API updates (POST new values, verify workers pick up changes)
- [ ] 13.7 Test Prometheus metrics are published correctly

## 14. Documentation

- [ ] 14.1 Add godoc comments to all exported types and functions in `pkg/syshealth/`
- [ ] 14.2 Create `apps/server-go/pkg/syshealth/README.md` with usage examples
- [ ] 14.3 Document health score calculation formula and thresholds
- [ ] 14.4 Document configuration API changes in API documentation
- [ ] 14.5 Add example cURL commands for enabling/disabling adaptive scaling
- [ ] 14.6 Document Prometheus metrics and their labels

## 15. Deployment Preparation

- [ ] 15.1 Verify hot reload works (code changes picked up without restart)
- [ ] 15.2 Run `nx run server-go:test` to ensure all tests pass
- [ ] 15.3 Run `nx run server-go:lint` to ensure code style compliance
- [ ] 15.4 Verify no breaking changes to existing worker configuration
- [ ] 15.5 Verify default behavior unchanged (adaptive scaling disabled by default)
- [ ] 15.6 Test configuration API backward compatibility (legacy fields still work)
- [ ] 15.7 Create deployment checklist for gradual rollout

## 16. Monitoring Setup

- [ ] 16.1 Create Grafana dashboard for system health monitoring
- [ ] 16.2 Add panel for health score over time with zone threshold lines
- [ ] 16.3 Add panel for individual metrics (CPU, I/O wait, Memory, DB pool)
- [ ] 16.4 Add panel for per-worker current concurrency
- [ ] 16.5 Add panel for concurrency adjustment events (counter rate)
- [ ] 16.6 Add panel for throttled jobs counter (rate)
- [ ] 16.7 Set up alert: Health score < 33 for > 5 minutes
- [ ] 16.8 Set up alert: Health monitor unavailable
- [ ] 16.9 Set up alert: Worker concurrency at minimum for > 10 minutes
