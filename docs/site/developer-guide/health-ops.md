# Health & Ops

The server exposes health check, readiness, diagnostics, and debug endpoints for use by load balancers, Kubernetes probes, and on-call engineers.

## Endpoints overview

| Endpoint | Purpose | Auth |
|---|---|---|
| `GET /health` | Full health status with checks | None |
| `GET /healthz` | Minimal liveness probe | None |
| `GET /ready` | Readiness probe | None |
| `GET /debug` | Go runtime stats (dev only) | None |
| `GET /api/diagnostics` | Deep DB diagnostics | None |

All health endpoints are unauthenticated and should be accessible from your load balancer or monitoring system.

---

## `/health` — Full health check

Returns a JSON object with overall status and per-subsystem checks.

```bash
curl https://api.dev.emergent-company.ai/health
```

```json
{
  "status": "healthy",
  "timestamp": "2026-03-08T12:00:00Z",
  "uptime": "3h12m5s",
  "version": "1.2.3",
  "checks": {
    "database": "healthy"
  }
}
```

Returns `200` when healthy. Returns `503 Service Unavailable` when any check fails.

---

## `/healthz` — Minimal liveness probe

Returns plain text `OK` (200) or `Service Unavailable` (503). Use this for high-frequency Kubernetes liveness probes where JSON parsing overhead is undesirable.

```bash
curl https://api.dev.emergent-company.ai/healthz
# OK
```

---

## `/ready` — Readiness probe

Returns a JSON object indicating whether the server is ready to receive traffic. The server may be alive (`/healthz`) but not yet ready (e.g. still running migrations).

```bash
curl https://api.dev.emergent-company.ai/ready
```

```json
{ "status": "ready" }
```

or

```json
{ "status": "not_ready", "message": "database migrations pending" }
```

---

## `/debug` — Runtime stats

!!! warning "Development only"
    This endpoint is only available when the server is started in development mode. Do not expose it in production.

Returns Go runtime memory statistics and database connection pool stats as JSON.

```bash
curl http://localhost:3012/debug
```

```json
{
  "go_runtime": {
    "goroutines": 42,
    "heap_alloc_mb": 128,
    "heap_sys_mb": 256,
    "gc_runs": 15
  },
  "db_pool": {
    "open_connections": 8,
    "in_use": 2,
    "idle": 6,
    "wait_count": 0
  }
}
```

---

## `/api/diagnostics` — Deep diagnostics

The diagnostics endpoint provides live database pool inspection and slow-query detection. Useful for on-call investigation of performance issues.

```bash
curl https://api.dev.emergent-company.ai/api/diagnostics
```

Response fields:

| Field | Description |
|---|---|
| `db_pool` | Bun connection pool statistics |
| `pg_stat_activity` | Active Postgres sessions from `pg_stat_activity` |
| `long_queries` | Queries running longer than 5 seconds |
| `settings` | Relevant PostgreSQL configuration settings |
| `table_sizes` | Top tables by disk size |

Example response:

```json
{
  "db_pool": {
    "open_connections": 10,
    "in_use": 3,
    "idle": 7
  },
  "pg_stat_activity": [
    {
      "pid": 1234,
      "state": "active",
      "query": "SELECT ...",
      "duration_sec": 0.1
    }
  ],
  "long_queries": [],
  "settings": {
    "max_connections": "100",
    "shared_buffers": "128MB"
  },
  "table_sizes": [
    { "table": "kb.graph_objects", "size": "4210 MB" },
    { "table": "kb.chunks", "size": "1024 MB" }
  ]
}
```

---

## OpenAPI specification

The server auto-generates an OpenAPI 3.0 specification. Access it at:

```
GET /api/docs/openapi.json
```

or in the admin UI:

```
https://admin.dev.emergent-company.ai/api-reference
```

You can import the spec into Postman, Insomnia, or any OpenAPI-compatible tool.

---

## Kubernetes probe configuration

Recommended Kubernetes probe settings:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 3012
  initialDelaySeconds: 10
  periodSeconds: 10
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /ready
    port: 3012
  initialDelaySeconds: 5
  periodSeconds: 5
  failureThreshold: 3
```

---

## Monitoring integration

For production monitoring, consider:

- **Uptime monitoring** — poll `/healthz` every 30s from an external probe
- **Alerting** — alert when `/health` returns non-healthy `checks`
- **Performance** — use `/api/diagnostics` to investigate slow queries when latency spikes
- **Tracing** — set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable OpenTelemetry tracing (no-op when unset)
