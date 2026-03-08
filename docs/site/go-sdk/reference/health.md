# health

Package `health` provides a client for the Emergent Health and Metrics APIs.

The health client is a **non-context client** — it requires no org or project context. It is used for infrastructure health checks (Kubernetes liveness/readiness probes), runtime diagnostics, and job queue metrics.

## Import

```go
import "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/health"
```

## Client

```go
type Client struct { /* unexported */ }
```

Obtained via `sdk.Client.Health`. No `SetContext` call is needed or available.

> **No authentication required** for the health/ready/healthz endpoints. Authentication _is_ required for `Debug`, `JobMetrics`, and `SchedulerStatus`.

---

## Methods

### Health

```go
func (c *Client) Health(ctx context.Context) (*HealthResponse, error)
```

Returns the overall service health including individual subsystem check results.
A `503 Service Unavailable` response is decoded normally (not treated as an error) since it represents the "unhealthy" state.

**Endpoint:** `GET /health`

---

### APIHealth

```go
func (c *Client) APIHealth(ctx context.Context) (*HealthResponse, error)
```

Same as `Health` but accessed via the API path (useful when the server is behind an API gateway that strips the root path).

**Endpoint:** `GET /api/health`

---

### Ready

```go
func (c *Client) Ready(ctx context.Context) (*ReadyResponse, error)
```

Returns the readiness state. A `503` response is still decoded (represents "not ready").
Use for Kubernetes **readiness probes** and load-balancer health checks.

**Endpoint:** `GET /ready`

---

### IsReady

```go
func (c *Client) IsReady(ctx context.Context) (bool, error)
```

Convenience wrapper around `Ready` that returns `true` only when `status == "ready"`.

---

### Healthz

```go
func (c *Client) Healthz(ctx context.Context) error
```

Simple liveness check. Returns `nil` on `200 OK`, an error otherwise.
Use for Kubernetes **liveness probes**.

**Endpoint:** `GET /healthz`

---

### Debug

```go
func (c *Client) Debug(ctx context.Context) (*DebugResponse, error)
```

Returns runtime debug info: Go version, goroutine count, memory stats, and database pool stats.

> **Note:** This endpoint is only available in non-production environments.

**Endpoint:** `GET /debug`

---

### JobMetrics

```go
func (c *Client) JobMetrics(ctx context.Context, projectID string) (*AllJobMetrics, error)
```

Returns per-queue job metrics. Pass `projectID` to scope metrics to a single project, or `""` for global metrics.

**Endpoint:** `GET /api/metrics/jobs`

---

### SchedulerStatus

```go
func (c *Client) SchedulerStatus(ctx context.Context) (*SchedulerMetrics, error)
```

Returns scheduled task metrics.

**Endpoint:** `GET /api/metrics/scheduler`

---

## Types

### HealthResponse

```go
type HealthResponse struct {
    Status    string           `json:"status"`
    Timestamp string           `json:"timestamp"`
    Uptime    string           `json:"uptime"`
    Version   string           `json:"version"`
    Checks    map[string]Check `json:"checks"`
}
```

`Status` is `"healthy"` or `"unhealthy"`. `Checks` maps subsystem names to their individual `Check` results.

### Check

```go
type Check struct {
    Status  string `json:"status"`
    Message string `json:"message,omitempty"`
}
```

---

### ReadyResponse

```go
type ReadyResponse struct {
    Status  string `json:"status"` // "ready" or "not_ready"
    Message string `json:"message,omitempty"`
}
```

---

### DebugResponse

```go
type DebugResponse struct {
    Environment string        `json:"environment"`
    Debug       bool          `json:"debug"`
    GoVersion   string        `json:"go_version"`
    Goroutines  int           `json:"goroutines"`
    Memory      DebugMemory   `json:"memory"`
    Database    DebugDatabase `json:"database"`
}
```

### DebugMemory

```go
type DebugMemory struct {
    AllocMB      uint64 `json:"alloc_mb"`
    TotalAllocMB uint64 `json:"total_alloc_mb"`
    SysMB        uint64 `json:"sys_mb"`
    NumGC        uint32 `json:"num_gc"`
}
```

### DebugDatabase

```go
type DebugDatabase struct {
    Host      string `json:"host"`
    Port      int    `json:"port"`
    Database  string `json:"database"`
    PoolTotal int32  `json:"pool_total"`
    PoolIdle  int32  `json:"pool_idle"`
    PoolInUse int32  `json:"pool_in_use"`
}
```

---

### JobQueueMetrics

```go
type JobQueueMetrics struct {
    Queue       string `json:"queue"`
    Pending     int64  `json:"pending"`
    Processing  int64  `json:"processing"`
    Completed   int64  `json:"completed"`
    Failed      int64  `json:"failed"`
    Total       int64  `json:"total"`
    LastHour    int64  `json:"last_hour"`
    Last24Hours int64  `json:"last_24_hours"`
}
```

### AllJobMetrics

```go
type AllJobMetrics struct {
    Queues    []JobQueueMetrics `json:"queues"`
    Timestamp string            `json:"timestamp"`
}
```

---

### SchedulerMetrics

```go
type SchedulerMetrics struct {
    Message string `json:"message,omitempty"`
}
```

---

## Example

```go
// Kubernetes readiness probe
ok, err := client.Health.IsReady(ctx)
if err != nil || !ok {
    // service is not ready
}

// Check detailed health
h, err := client.Health.Health(ctx)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Status: %s (uptime: %s)\n", h.Status, h.Uptime)
for name, check := range h.Checks {
    fmt.Printf("  %s: %s\n", name, check.Status)
}

// Check job queue backlog
metrics, err := client.Health.JobMetrics(ctx, "")
if err != nil {
    log.Fatal(err)
}
for _, q := range metrics.Queues {
    fmt.Printf("Queue %s: %d pending, %d failed\n", q.Queue, q.Pending, q.Failed)
}
```
