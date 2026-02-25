# worker-scaling-observability Specification

## Purpose

Exposes Prometheus metrics for system health scores, component metrics (CPU, I/O, DB, Memory), and worker-specific concurrency adjustments. Defines the required alerts and dashboards for operational visibility.

## ADDED Requirements

### Requirement: Health Component Metrics

The system SHALL expose system-level performance indicators to Prometheus via existing `/metrics` endpoint.

#### Scenario: Exporting CPU load

- **WHEN** the system collects CPU metrics
- **THEN** it exposes `system_cpu_load_avg` labeled with period (1m, 5m, 15m)

#### Scenario: Exporting Resource Utilization Percentages

- **WHEN** the system collects percentage-based metrics
- **THEN** it exposes `system_io_wait_percent`, `system_memory_utilization_percent`, and `system_db_pool_utilization_percent` as gauges (0-100)

### Requirement: Health Score Metrics

The system SHALL export the calculated health score to help operators track overall system health.

#### Scenario: Exporting the overall health score

- **WHEN** the system calculates the system health score
- **THEN** it exposes `system_health_score` gauge labeled with the current `zone` (critical, warning, safe)

### Requirement: Worker Concurrency Metrics

The system SHALL track how workers dynamically adjust their concurrency limits over time.

#### Scenario: Exporting active concurrency limit

- **WHEN** a worker's concurrency limit is calculated or adjusted
- **THEN** it exposes `extraction_worker_current_concurrency` labeled by `worker_type`

#### Scenario: Exporting concurrency adjustments

- **WHEN** a worker adjusts its concurrency up or down
- **THEN** it increments a counter `extraction_worker_concurrency_adjustments_total`
- **AND** adds labels for `worker_type`, `direction` (increase/decrease), and `reason` (health_critical, health_warning, health_safe)

#### Scenario: Exporting throttled jobs

- **WHEN** a job is delayed or blocked due to reduced concurrency limits
- **THEN** it increments `extraction_jobs_throttled_total` labeled by `worker_type`
