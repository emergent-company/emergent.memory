## Why

After successfully implementing the core logic for dynamic worker scaling (health monitoring and concurrency adjustment), we need to ensure the system is properly observable, testable, and safe to deploy. Production resource exhaustion requires careful tuning, which is impossible without visibility into health scores and concurrency adjustments. Comprehensive testing and monitoring are essential before enabling this feature on critical production workers.

## What Changes

- **Prometheus Metrics Integration**: Export health scores, system metrics, and worker concurrency levels to existing Prometheus infrastructure.
- **Comprehensive Testing**: Implement unit tests for the health monitor and concurrency scaler, plus integration tests for worker behavior.
- **Observability Dashboards**: Define the requirements for Grafana dashboards and alerts to monitor scaling behavior and system health.
- **Deployment & Documentation**: Finalize API documentation, deployment checklists, and operational runbooks for gradual rollout.

## Capabilities

### New Capabilities

- `worker-scaling-observability`: Exposes Prometheus metrics for system health scores, component metrics (CPU, I/O, DB, Memory), and worker-specific concurrency adjustments. Defines the required alerts and dashboards for operational visibility.
- `worker-scaling-validation`: Establishes the testing requirements (unit and integration) to ensure adaptive scaling behaves predictably under simulated load and resource pressure.

### Modified Capabilities

- `entity-extraction`: Update operational requirements for extraction workers to include observability and monitoring standards during the adaptive scaling rollout phase.

## Impact

**Code Changes:**

- New package: `pkg/syshealth/metrics.go` for Prometheus metric registration.
- New test files: `monitor_test.go`, `scaler_test.go`, `integration_test.go`.
- Modified: Worker tests to use mock scalers.

**Dependencies:**

- None new (using existing Prometheus client library).

**Infrastructure:**

- New metrics exposed on existing `/metrics` endpoint.
- New Grafana dashboard and Prometheus alerts required.

**Operational:**

- Provides the necessary visibility to safely enable adaptive scaling in production.
- Establishes alerting thresholds for critical system health events.
