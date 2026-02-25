## Why

Production servers are experiencing critical resource exhaustion due to aggressive worker job processing. The mcj-emergent server shows load averages of 4-30+ (should be <4), 42-54% I/O wait causing Docker/MinIO operations to hang, and multiple postgres processes stuck in I/O wait. Current workers use hardcoded concurrency (default 10) and process jobs at maximum rate regardless of system health, leading to disk I/O exhaustion, high CPU context switching, and service timeouts. We need adaptive worker scaling to maintain system stability while continuing to process jobs at a sustainable rate.

## What Changes

- **New system health monitoring service** that collects CPU load, I/O wait percentage, memory pressure, and database connection pool metrics every 30 seconds
- **Adaptive concurrency controller** that dynamically adjusts worker concurrency (1-10 concurrent jobs per worker) based on system health score (0-100)
- **Health-aware worker integration** for all 4 existing worker types (GraphEmbedding, ChunkEmbedding, DocumentParsing, ObjectExtraction) using a minimal mixin pattern
- **Configurable scaling thresholds** with safe defaults (critical I/O wait >40%, warning >30%, safe <20%)
- **Graceful degradation** that automatically reduces concurrency when system is unhealthy and increases when recovered
- **Prometheus metrics** for health scores, current concurrency per worker, and scaling events
- **Runtime configuration API** extending existing `/admin/extraction/embedding/config` to control adaptive scaling per worker type
- **Feature flags** for gradual rollout (disabled by default, enable per worker type)

## Capabilities

### New Capabilities

- `system-health-monitoring`: Collects and exposes system resource metrics (CPU load average, I/O wait %, memory pressure, DB connection pool usage) to determine overall system health score for scaling decisions

- `adaptive-worker-concurrency`: Dynamically adjusts worker job processing concurrency based on real-time system health, with configurable min/max bounds, scaling rules, and cooldown periods to prevent resource exhaustion

### Modified Capabilities

- `entity-extraction`: Extend existing extraction worker configuration to support adaptive scaling toggle, min/max concurrency bounds, and integration with health monitor for dynamic concurrency adjustments

## Impact

**Code Changes:**

- New package: `apps/server-go/pkg/syshealth/` (~300 lines) for health monitoring
- Modified: All 4 worker files in `apps/server-go/domain/extraction/` (~10 lines each)
- Extended: Worker config structs to include adaptive scaling settings
- Modified: `domain/extraction/module.go` to wire up health monitor via fx

**Dependencies:**

- Add `github.com/shirou/gopsutil/v3` for cross-platform system metrics (industry standard, 12k+ stars)

**Infrastructure:**

- Prometheus metrics exposed at existing `/metrics` endpoint (no new ports)
- Logs: New health monitor logs at INFO level (30s intervals)

**APIs:**

- Extend `POST /admin/extraction/embedding/config` to accept new fields: `enable_adaptive_scaling`, `min_concurrency`, `max_concurrency`
- No breaking changes - all new fields are optional with backward-compatible defaults

**Operational:**

- Default behavior unchanged (adaptive scaling disabled by default)
- Gradual rollout: Enable per worker type via config API
- Monitoring: New Grafana dashboard for health scores and dynamic concurrency
- Rollback: Set `enable_adaptive_scaling: false` via API (immediate effect)

**Testing:**

- Unit tests: Mock system metrics for health monitor
- Integration tests: Verify concurrency adjustments under simulated load
- Production validation: Enable on ChunkEmbedding worker first (least critical), monitor 1-2 weeks before broader rollout
