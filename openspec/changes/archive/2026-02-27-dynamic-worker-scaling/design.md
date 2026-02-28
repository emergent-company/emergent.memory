# Dynamic Worker Scaling Design

## Context

### Current State

Production servers are experiencing critical resource exhaustion with load averages of 4-30+ and 42-54% I/O wait, causing Docker/MinIO operations to hang. The root cause is workers processing jobs at maximum rate (default 10 concurrent jobs per worker) regardless of system health.

**Existing Worker Architecture:**

- 4 worker types in `apps/server-go/domain/extraction/`: GraphEmbedding, ChunkEmbedding, DocumentParsing, ObjectExtraction
- Polling-based job processing with configurable intervals (default 30s)
- Semaphore-based concurrency control using buffered channels (line 192 in `graph_embedding_worker.go`)
- Runtime configuration API at `/admin/extraction/embedding/config` for batch size and concurrency
- Uber FX for dependency injection
- Workers already structured with good separation: service layer for job management, worker layer for execution

**Constraints:**

- Must maintain existing worker architecture and polling patterns
- Cannot introduce breaking changes to worker configuration
- Must support gradual rollout (enable per worker type)
- Default behavior must remain unchanged (adaptive scaling off by default)
- Hot reload should work without requiring restarts

### Stakeholders

- **Operations team**: Needs immediate relief from production resource exhaustion
- **Development team**: Wants minimal disruption to existing worker codebase
- **Platform team**: Concerned about observability and rollback capabilities

## Goals / Non-Goals

**Goals:**

- Prevent production resource exhaustion by dynamically adjusting worker concurrency based on system health
- Maintain backward compatibility with existing worker configuration
- Enable gradual rollout with per-worker feature flags
- Provide rich observability (Prometheus metrics, structured logging)
- Support immediate rollback via API (no code deploy required)
- Keep integration minimal (~10 lines per worker)

**Non-Goals:**

- Rewriting existing worker architecture or job queue systems
- Queue-depth-based scaling (focusing on system health, not queue length)
- Distributed tracing of individual job execution (out of scope)
- Auto-scaling infrastructure resources (containers, VMs) - only worker concurrency
- Real-time metric streaming (30s collection interval is sufficient)
- Multi-region health coordination (single-server focus)

## Decisions

### Decision 1: Custom Lightweight Solution vs External Library

**Chosen**: Custom health monitoring + concurrency controller (~300 lines in `pkg/syshealth/`)

**Rationale**:

- Existing workers follow best practices and are well-structured
- Integration with libraries like `go-adaptive-pool` would require major refactoring (replacing semaphore pattern, changing worker lifecycle)
- Need system-health-based throttling (I/O wait, DB connections), not just queue-depth scaling
- Custom solution provides fine-grained control with faster deployment (2-3 days vs 2+ weeks)
- Keeps dependency footprint minimal (only adding `gopsutil`)

**Alternatives Considered**:

- **`github.com/panjf2000/ants` (goroutine pool)**: Focuses on goroutine reuse, not health-aware scaling. Would require replacing existing semaphore pattern.
- **`github.com/gammazero/workerpool`**: No health monitoring. Would need wrapper layer, adding complexity.
- **Full backpressure system (queue-based)**: Over-engineered for this use case. Problem is resource exhaustion, not queue management.

### Decision 2: Health Monitoring Architecture

**Chosen**: Standalone health monitor service with periodic metric collection (30s interval)

**Implementation**:

```go
// pkg/syshealth/monitor.go
type Monitor struct {
    config   *Config
    metrics  *HealthMetrics
    mu       sync.RWMutex
    ticker   *time.Ticker
    stopCh   chan struct{}
}

type HealthMetrics struct {
    Score           int       // 0-100
    Zone            HealthZone // critical/warning/safe
    CPULoadAvg      float64   // 1-minute load average
    IOWaitPercent   float64   // 0-100
    MemoryPercent   float64   // 0-100
    DBPoolPercent   float64   // 0-100
    Timestamp       time.Time
    Stale           bool
}
```

**Rationale**:

- Polling-based monitoring matches worker polling patterns (consistency)
- 30-second interval provides timely response without excessive overhead
- Centralized service provides single source of truth for all workers
- Read-heavy pattern (many workers querying, one monitor writing) fits RWMutex well

**Alternatives Considered**:

- **Push-based metrics**: More complex, requires workers to push updates. Adds coordination overhead.
- **Per-worker health monitoring**: Duplicates metric collection, inconsistent health scores across workers.
- **Real-time streaming**: Over-engineered. 30s granularity is sufficient for worker scaling decisions.

### Decision 3: Health Score Calculation Formula

**Chosen**: Weighted scoring with I/O wait as highest priority

```
Health Score = 100 - (
    IOWaitScore * 0.4 +
    CPULoadScore * 0.3 +
    DBPoolScore * 0.2 +
    MemoryScore * 0.1
)

Where each component score (0-100) is calculated:
- IOWaitScore: 0 if <20%, 50 if 20-40%, 100 if >40%
- CPULoadScore: 0 if <2x cores, 50 if 2-3x, 100 if >3x
- DBPoolScore: 0 if <75%, 50 if 75-90%, 100 if >90%
- MemoryScore: 0 if <85%, 50 if 85-95%, 100 if >95%
```

**Rationale**:

- I/O wait is the primary symptom in production (42-54%), so it gets highest weight (40%)
- CPU load is secondary indicator of overall system pressure (30%)
- DB connection pool exhaustion can cascade failures (20%)
- Memory is lowest priority since production shows adequate available memory (10%)
- Weighted approach prevents single metric from dominating score

**Alternatives Considered**:

- **"Worst metric wins" (min score)**: Too aggressive. Single spike would throttle all workers unnecessarily.
- **Equal weighting**: Doesn't reflect production reality where I/O wait is the critical bottleneck.
- **Machine learning-based**: Over-engineered. Static thresholds are interpretable and sufficient.

### Decision 4: Concurrency Adjustment Strategy

**Chosen**: Three-zone scaling with gradual recovery

| Health Zone | Score Range | Target Concurrency | Cooldown         |
| ----------- | ----------- | ------------------ | ---------------- |
| Critical    | 0-33        | min (default: 1)   | None (immediate) |
| Warning     | 34-66       | 50% of max         | 1 minute         |
| Safe        | 67-100      | max (default: 10)  | 5 minutes        |

**Gradual scaling rules**:

- **Downward**: Apply immediately (within one polling cycle)
- **Upward**: Increase by max 50% per cycle, respect cooldown period
- **Critical bypass**: Skip cooldown when entering critical zone

**Rationale**:

- Asymmetric response: Fast throttling, slow recovery (prevents oscillation)
- Three zones provide clear actionable states
- 50% increments prevent abrupt changes that could destabilize recovered systems
- Cooldown periods prevent rapid oscillation from transient spikes
- Critical zone bypass ensures immediate response to severe conditions

**Alternatives Considered**:

- **Linear scaling (health score → concurrency)**: Too reactive. Small score changes would constantly adjust concurrency.
- **PID controller**: Over-engineered. Tuning is complex and our workload doesn't require such precision.
- **Fixed step increments (±1)**: Too slow to recover. Would take 10 cycles to scale from 1 to 10.

### Decision 5: Integration Pattern (Mixin vs Inheritance vs Wrapper)

**Chosen**: Health-aware mixin injected via dependency injection

```go
// pkg/syshealth/scaler.go
type ConcurrencyScaler struct {
    monitor      *Monitor
    minConcurrency int
    maxConcurrency int
    enabled        bool
}

func (s *ConcurrencyScaler) GetConcurrency(staticValue int) int {
    if !s.enabled {
        return staticValue // Legacy behavior
    }
    health := s.monitor.GetHealth()
    // Calculate and return adjusted concurrency
}

// In worker (e.g., graph_embedding_worker.go line 188):
// BEFORE: concurrency := w.cfg.WorkerConcurrency
// AFTER:  concurrency := w.scaler.GetConcurrency(w.cfg.WorkerConcurrency)
```

**Rationale**:

- Minimal code changes (~10 lines per worker)
- Preserves existing semaphore pattern (line 192 in workers)
- Maintains backward compatibility (if disabled, returns static value)
- Testable in isolation (mock health monitor)
- Fits existing FX dependency injection pattern

**Alternatives Considered**:

- **Base worker class with health awareness**: Go doesn't have inheritance. Would require interface changes, breaking existing code.
- **Wrapper around entire worker**: Would need to intercept job fetching, polling loop, etc. Too invasive.
- **Middleware pattern**: Doesn't fit polling-based workers. Better suited for request/response systems.

### Decision 6: Configuration Storage and Runtime Updates

**Chosen**: In-memory configuration with database persistence and runtime API

**Storage**:

- Configuration stored in worker config structs (existing pattern)
- Persisted to database via existing config tables
- Loaded on worker startup, updated via API calls

**API Extension** (`/admin/extraction/embedding/config`):

```json
{
  "worker_concurrency": 10, // Legacy field (backward compatible)
  "enable_adaptive_scaling": false, // New field (default: false)
  "min_concurrency": 1, // New field (default: 1)
  "max_concurrency": 10 // New field (default: 10)
}
```

**Rationale**:

- Extends existing API instead of creating new endpoint
- Backward compatible (new fields are optional)
- Runtime updates without code deployment
- Database persistence survives restarts

**Alternatives Considered**:

- **Environment variables**: Requires restart to change. Doesn't support per-worker configuration.
- **Separate configuration service**: Over-engineered. Existing config system works well.
- **Feature flag service (LaunchDarkly)**: External dependency. Overkill for on/off toggle.

### Decision 7: System Metrics Library

**Chosen**: `github.com/shirou/gopsutil/v3`

**Rationale**:

- Industry standard for Go system metrics (12k+ stars, 2k+ forks)
- Cross-platform (Linux, macOS, Windows)
- Well-maintained (active development, frequent releases)
- Comprehensive API (CPU, memory, disk, load average, I/O stats)
- Production-proven (used by Kubernetes, Prometheus node exporter, etc.)

**Alternatives Considered**:

- **`/proc` filesystem parsing**: Linux-only. Brittle. Would need platform-specific code for macOS dev environments.
- **Shell command execution (`iostat`, `vmstat`)**: Unreliable. Parsing stdout is fragile. High overhead.
- **`golang.org/x/sys/unix`**: Too low-level. Would need to implement metric calculation ourselves.

### Decision 8: Observability Strategy

**Chosen**: Structured logging + Prometheus metrics + existing `/metrics` endpoint

**Metrics to expose**:

```
# Health monitoring
system_health_score{zone="safe|warning|critical"}         # Gauge 0-100
system_io_wait_percent                                     # Gauge 0-100
system_cpu_load_avg{period="1m|5m|15m"}                   # Gauge
system_memory_utilization_percent                          # Gauge 0-100
system_db_pool_utilization_percent                         # Gauge 0-100

# Worker concurrency
extraction_worker_current_concurrency{worker_type="..."}   # Gauge
extraction_worker_concurrency_adjustments_total{worker_type="...",direction="increase|decrease",reason="..."}  # Counter
extraction_jobs_throttled_total{worker_type="..."}         # Counter
```

**Logging levels**:

- INFO: Periodic health status (every 30s), concurrency adjustments
- WARN: Health zone transitions, stale metrics
- ERROR: Metric collection failures
- DEBUG: Health score calculations, throttled jobs

**Rationale**:

- Prometheus is already deployed and used for monitoring
- Structured logging with `slog` provides rich context for debugging
- Metrics enable alerting (e.g., "alert if health score < 33 for > 5 minutes")
- No new infrastructure required

**Alternatives Considered**:

- **Custom metrics dashboard**: Redundant. Prometheus + Grafana is standard.
- **Trace-based monitoring**: Overkill. Metrics and logs are sufficient for worker scaling decisions.
- **Push gateway**: Not needed. `/metrics` endpoint is scraped by existing Prometheus.

## Risks / Trade-offs

### Risk 1: Health Monitor Availability

**Risk**: If health monitor crashes or becomes unresponsive, workers might not scale properly.

**Mitigation**:

- Workers use last known health score if monitor doesn't respond within 5 seconds
- If health data is stale (>2 minutes), workers assume moderate health (score 50) and use 50% of max concurrency
- Health monitor is simple and stateless, unlikely to crash
- Monitor failure is logged at ERROR level, alerting operations team

### Risk 2: Metric Collection Overhead

**Risk**: Collecting system metrics every 30 seconds could add CPU overhead.

**Mitigation**:

- `gopsutil` is optimized for performance (used by Prometheus node exporter)
- Metric collection runs in separate goroutine, doesn't block workers
- 30-second interval is intentionally chosen to balance timeliness and overhead
- If collection takes >5 seconds (timeout), log warning and use last known values

### Risk 3: Concurrency Oscillation

**Risk**: If health score fluctuates rapidly, concurrency could oscillate, destabilizing workers.

**Mitigation**:

- Asymmetric cooldown periods: 5 minutes for increases, 1 minute for decreases
- Gradual scaling: Max 50% increase per cycle
- Weighted health score calculation smooths transient spikes
- Critical zone bypasses cooldown (immediate response to severe conditions)

### Risk 4: Starvation During Extended Unhealthy Periods

**Risk**: If system remains unhealthy for extended periods, jobs might not get processed.

**Mitigation**:

- Minimum concurrency floor (default: 1) ensures at least some progress
- Jobs remain in queue and will be processed when health recovers
- Operators can temporarily disable adaptive scaling via API if needed
- Alerting on prolonged critical health enables manual intervention

### Risk 5: False Positives from Transient Spikes

**Risk**: Brief I/O spikes (e.g., database backup) could unnecessarily throttle workers.

**Mitigation**:

- Weighted scoring prevents single metric from dominating
- 30-second collection interval averages out sub-30s spikes
- Cooldown periods prevent immediate re-scaling after brief recovery
- Operators can tune thresholds per environment (e.g., higher I/O wait tolerance in dev)

### Risk 6: Configuration Complexity

**Risk**: Additional configuration fields increase cognitive load for operators.

**Mitigation**:

- Sane defaults (disabled by default, min=1, max=10)
- Backward compatible (existing config continues to work)
- API validates constraints (min ≥ 1, max ≥ min, max ≤ 50)
- Clear documentation and examples in API response
- Gradual rollout reduces risk (enable one worker at a time)

### Risk 7: Database Connection Pool Metric Accuracy

**Risk**: Bun ORM connection pool stats might not reflect true database load.

**Mitigation**:

- Use Bun's `db.Stats()` method which provides accurate pool statistics
- Cross-reference with I/O wait and CPU load for holistic view
- Database pool utilization has lower weight (20%) in health score
- If Bun stats are unavailable, gracefully degrade (use last known value)

## Migration Plan

### Phase 1: Implementation (Week 1)

1. Implement `pkg/syshealth/monitor.go` with metric collection and health score calculation
2. Implement `pkg/syshealth/scaler.go` with concurrency adjustment logic
3. Add `gopsutil` dependency: `go get github.com/shirou/gopsutil/v3`
4. Wire up health monitor in `domain/extraction/module.go` via FX
5. Unit tests for health monitor and scaler (mock metrics)

### Phase 2: Worker Integration (Week 1)

1. Extend worker config structs with adaptive scaling fields
2. Integrate scaler into all 4 workers (~10 lines each)
3. Extend `/admin/extraction/embedding/config` API handler
4. Add Prometheus metrics export
5. Integration tests simulating health score changes

### Phase 3: Gradual Production Rollout (Weeks 2-4)

1. **Week 2**: Deploy to production with adaptive scaling disabled (verify monitoring works)
2. **Week 2**: Enable adaptive scaling for ChunkEmbedding worker only (least critical, 229 failed jobs)
   - Monitor for 3-5 days
   - Verify concurrency adjustments correlate with health scores
   - Check for job processing delays
3. **Week 3**: Enable for DocumentParsing worker (low volume, 25 jobs total)
   - Monitor for 2-3 days
4. **Week 3-4**: Enable for GraphEmbedding worker (high volume, 886 completed jobs)
   - Monitor closely for 1 week
   - This is the critical worker, most likely to impact production
5. **Week 4**: Enable for ObjectExtraction worker (completes rollout)

### Phase 4: Monitoring and Tuning (Ongoing)

1. Create Grafana dashboard with:
   - Health score over time (with zone thresholds)
   - Per-worker current concurrency
   - Job processing rates vs health score
   - Concurrency adjustment events
2. Set up alerts:
   - Health score < 33 for > 5 minutes (critical)
   - Health monitor unavailable (error)
   - Worker concurrency at minimum for > 10 minutes (warning)
3. Review thresholds after 2 weeks:
   - Adjust I/O wait thresholds if too sensitive/insensitive
   - Tune cooldown periods if oscillation observed
   - Adjust min/max concurrency per worker type based on production behavior

### Rollback Strategy

**Immediate rollback (seconds)**:

- Disable adaptive scaling via API: `POST /admin/extraction/embedding/config` with `enable_adaptive_scaling: false`
- Workers revert to static concurrency on next polling cycle (≤30s)

**Full rollback (minutes)**:

- Redeploy previous version without health monitoring code
- Configuration API remains backward compatible

**Partial rollback**:

- Disable adaptive scaling for specific problematic worker types
- Keep monitoring in place for observability

## Open Questions

1. **Should we expose health score via a dedicated `/health/score` endpoint?**

   - Pro: Easier for external monitoring systems to query
   - Con: `/metrics` endpoint already exposes this data
   - Recommendation: Start with `/metrics` only, add dedicated endpoint if requested by ops team

2. **Should cooldown periods be configurable per worker type?**

   - Pro: Flexibility for different worker characteristics (e.g., fast-recovering workers)
   - Con: Increases configuration complexity
   - Recommendation: Use global defaults initially, make configurable if needed after rollout

3. **Should we track job queue depth alongside system health?**

   - Pro: Could inform more intelligent scaling decisions
   - Con: Problem is resource exhaustion, not queue backlog
   - Recommendation: Out of scope for initial implementation. Revisit if queue depth becomes an issue.

4. **Should we implement circuit breakers for individual worker types?**

   - Pro: Automatically disable problematic workers
   - Con: Adds complexity, requires defining "problematic" thresholds
   - Recommendation: Implement manual disable via API initially. Add circuit breaker if pattern emerges.

5. **How should we handle multi-instance deployments (future)?**
   - Pro: Per-instance health monitoring could prevent cascading failures
   - Con: Current deployment is single-instance
   - Recommendation: Design for single-instance now, revisit if multi-instance deployment is planned.
