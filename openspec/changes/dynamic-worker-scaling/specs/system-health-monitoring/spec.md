# system-health-monitoring Specification

## Purpose

Provides real-time system resource monitoring to inform adaptive worker scaling decisions. Collects CPU load average, I/O wait percentage, memory pressure, and database connection pool metrics to calculate an overall system health score (0-100) that workers use to dynamically adjust their concurrency.

## ADDED Requirements

### Requirement: System Metrics Collection

The system SHALL collect key system resource metrics at regular intervals to assess overall system health.

#### Scenario: CPU load average collection

- **WHEN** the health monitor runs its collection cycle
- **THEN** the system collects the 1-minute, 5-minute, and 15-minute load averages
- **AND** normalizes load average by CPU count (e.g., load 8.0 on 4 cores = 200% load)

#### Scenario: I/O wait percentage collection

- **WHEN** the health monitor runs its collection cycle
- **THEN** the system collects the current I/O wait percentage
- **AND** records the percentage as a value between 0-100

#### Scenario: Memory pressure collection

- **WHEN** the health monitor runs its collection cycle
- **THEN** the system collects total and available memory
- **AND** calculates memory utilization percentage
- **AND** records swap usage if applicable

#### Scenario: Database connection pool metrics

- **WHEN** the health monitor runs its collection cycle
- **THEN** the system collects the number of active database connections
- **AND** collects the maximum allowed connections from the pool configuration
- **AND** calculates connection pool utilization percentage

### Requirement: Configurable Collection Interval

The system SHALL allow operators to configure how frequently system metrics are collected.

#### Scenario: Default collection interval

- **WHEN** the health monitor starts without explicit configuration
- **THEN** the system collects metrics every 30 seconds by default

#### Scenario: Custom collection interval

- **WHEN** an operator configures a custom collection interval (e.g., 15 seconds)
- **THEN** the system collects metrics at the specified interval
- **AND** validates that the interval is at least 5 seconds to prevent excessive overhead

### Requirement: Health Score Calculation

The system SHALL calculate an overall health score (0-100) based on collected metrics, with configurable thresholds for critical, warning, and safe zones.

#### Scenario: Critical health conditions

- **WHEN** I/O wait exceeds the critical threshold (default 40%)
- **OR** CPU load exceeds 3x the CPU count
- **OR** memory utilization exceeds 95%
- **OR** database connection pool utilization exceeds 90%
- **THEN** the system assigns a health score in the critical range (0-33)

#### Scenario: Warning health conditions

- **WHEN** I/O wait is between warning (default 30%) and critical (default 40%) thresholds
- **OR** CPU load is between 2x and 3x the CPU count
- **OR** memory utilization is between 85% and 95%
- **OR** database connection pool utilization is between 75% and 90%
- **THEN** the system assigns a health score in the warning range (34-66)

#### Scenario: Safe health conditions

- **WHEN** I/O wait is below the warning threshold (default 30%)
- **AND** CPU load is below 2x the CPU count
- **AND** memory utilization is below 85%
- **AND** database connection pool utilization is below 75%
- **THEN** the system assigns a health score in the safe range (67-100)

#### Scenario: Weighted scoring

- **WHEN** calculating the overall health score
- **THEN** the system weights I/O wait as the highest priority factor (40% weight)
- **AND** weights CPU load as secondary (30% weight)
- **AND** weights database connections as tertiary (20% weight)
- **AND** weights memory utilization as lowest priority (10% weight)

### Requirement: Health Status API

The system SHALL expose current health metrics and scores via an API endpoint for monitoring and debugging.

#### Scenario: Query current health status

- **WHEN** an operator queries the health status endpoint
- **THEN** the system returns the current health score (0-100)
- **AND** returns all individual metric values (CPU load, I/O wait %, memory %, DB pool %)
- **AND** returns the timestamp of the most recent metric collection
- **AND** returns the current health zone (critical/warning/safe)

#### Scenario: Historical health data

- **WHEN** an operator queries the health status endpoint with a time range
- **THEN** the system returns health scores for the requested time period
- **AND** includes min, max, and average health scores for the period

### Requirement: Health Metrics Logging

The system SHALL log health metrics and score changes at appropriate log levels.

#### Scenario: Periodic health logging

- **WHEN** the health monitor completes a metrics collection cycle
- **THEN** the system logs the current health score and zone at INFO level
- **AND** includes key metric values (I/O wait %, CPU load, DB pool %)

#### Scenario: Health zone transitions

- **WHEN** the health score crosses a zone boundary (safe ↔ warning ↔ critical)
- **THEN** the system logs the transition at WARN level
- **AND** includes the previous and new health scores
- **AND** identifies which metric(s) triggered the transition

#### Scenario: Metric collection failures

- **WHEN** the system fails to collect one or more metrics
- **THEN** the system logs the failure at ERROR level
- **AND** uses the last known value for the failed metric(s)
- **AND** includes a staleness indicator in the health score response

### Requirement: Prometheus Metrics Export

The system SHALL expose health monitoring data as Prometheus metrics for integration with existing observability infrastructure.

#### Scenario: Health score gauge metric

- **WHEN** the health monitor updates the health score
- **THEN** the system updates a `system_health_score` gauge metric (0-100)
- **AND** adds a label for the current health zone (critical/warning/safe)

#### Scenario: Individual resource metrics

- **WHEN** the health monitor collects metrics
- **THEN** the system exposes each metric as a separate gauge:
  - `system_cpu_load_avg` (1m, 5m, 15m as separate series)
  - `system_io_wait_percent` (0-100)
  - `system_memory_utilization_percent` (0-100)
  - `system_db_pool_utilization_percent` (0-100)

#### Scenario: Health zone duration tracking

- **WHEN** the health monitor is running
- **THEN** the system tracks cumulative time spent in each health zone (critical/warning/safe)
- **AND** exposes the durations as counter metrics (seconds)

### Requirement: Graceful Degradation on Collection Errors

The system SHALL continue operating and providing best-effort health scores even when metric collection partially fails.

#### Scenario: Single metric collection failure

- **WHEN** one metric fails to collect (e.g., I/O wait unavailable)
- **THEN** the system continues using the last known value for that metric
- **AND** marks the metric as stale if it hasn't been updated in 5 minutes
- **AND** recalculates the health score using available fresh metrics with adjusted weights

#### Scenario: Complete collection failure

- **WHEN** all metrics fail to collect for a cycle
- **THEN** the system logs an ERROR
- **AND** returns the last known health score with a staleness warning
- **AND** retries metric collection on the next scheduled cycle

#### Scenario: Persistent collection failures

- **WHEN** metric collection fails for 3 consecutive cycles
- **THEN** the system logs a CRITICAL error
- **AND** defaults to a safe health score (50) to prevent aggressive worker scaling
- **AND** notifies operators via configured alerting channels
