# System Health Monitoring & Adaptive Scaling

The `syshealth` package provides infrastructure for monitoring system resource utilization and dynamically adjusting worker concurrency levels to maintain system stability.

## Core Components

### 1. Monitor
The `Monitor` collects system metrics (CPU Load, I/O Wait, Memory, and DB Pool usage) and calculates an overall health score (0-100).

- **Safe Zone (67-100)**: Normal operation, no resource pressure.
- **Warning Zone (34-66)**: Moderate resource pressure detected.
- **Critical Zone (0-33)**: Severe resource pressure; immediate action required.

### 2. ConcurrencyScaler
The `ConcurrencyScaler` is injected into workers and determines the allowed concurrency level based on the current health zone.

- **Cooldown Periods**: Prevents rapid oscillation by waiting 5 minutes before increasing concurrency and 1 minute before decreasing (except in Critical zone, where it drops immediately).
- **Gradual Scaling**: Concurrency increases are capped at 50% of the current level per cycle.

## Usage Example

### Initializing the Monitor

```go
cfg := syshealth.DefaultConfig()
monitor := syshealth.NewMonitor(cfg, db, logger)
monitor.Start()
defer monitor.Stop()
```

### Using the Scaler in a Worker

```go
scaler := syshealth.NewConcurrencyScaler(monitor, "my-worker", true, 5, 50)

func (w *MyWorker) run() {
    for {
        // Dynamically get allowed concurrency
        concurrency := scaler.GetConcurrency(w.staticConfigValue)
        
        sem := make(chan struct{}, concurrency)
        // ... process batch ...
    }
}
```

## Health Score Formula

The health score is calculated using a weighted penalty system:

| Metric | Weight | Warning Threshold | Critical Threshold |
|---|---|---|---|
| **I/O Wait** | 40% | 30% | 40% |
| **CPU Load** | 30% | 2x Cores | 3x Cores |
| **DB Pool** | 20% | 75% | 90% |
| **Memory** | 10% | 85% | 95% |

Each component contributes `100 * Weight` to the total penalty if it exceeds its critical threshold, or `50 * Weight` if it exceeds its warning threshold.

## Configuration API

The adaptive scaling behavior for embedding workers can be controlled via the `/api/embeddings/config` endpoint.

### Example: Enable Adaptive Scaling

```bash
curl -X PATCH http://localhost:8080/api/embeddings/config 
  -H "Content-Type: application/json" 
  -d '{
    "enable_adaptive_scaling": true,
    "min_concurrency": 10,
    "max_concurrency": 100
  }'
```
