# adaptive-worker-concurrency Specification

## Purpose

Dynamically adjusts worker job processing concurrency based on real-time system health to prevent resource exhaustion while maintaining sustainable job throughput. Integrates with system health monitoring to scale worker concurrency up during healthy conditions and down during resource pressure.

## ADDED Requirements

### Requirement: Dynamic Concurrency Adjustment

The system SHALL automatically adjust worker concurrency based on the current system health score.

#### Scenario: Concurrency reduction in critical health

- **WHEN** the system health score enters the critical zone (0-33)
- **THEN** the worker concurrency is reduced to minimum configured level (default: 1)
- **AND** the adjustment occurs within one polling cycle
- **AND** the system logs the concurrency change with the triggering health score

#### Scenario: Concurrency reduction in warning health

- **WHEN** the system health score is in the warning zone (34-66)
- **THEN** the worker concurrency is set to 50% of the maximum configured level
- **AND** the adjustment respects the minimum concurrency bound
- **AND** the system logs the concurrency change with the triggering health score

#### Scenario: Concurrency increase in safe health

- **WHEN** the system health score is in the safe zone (67-100)
- **THEN** the worker concurrency is set to the maximum configured level
- **AND** the adjustment occurs gradually over multiple cycles (not immediately)
- **AND** the system logs the concurrency change with the triggering health score

#### Scenario: Gradual scaling with cooldown

- **WHEN** adjusting concurrency upward after health recovery
- **THEN** the system waits for a cooldown period (default: 5 minutes) before increasing
- **AND** increases concurrency by at most 50% per adjustment cycle
- **AND** continues gradual increases until maximum concurrency is reached

### Requirement: Configurable Concurrency Bounds

The system SHALL allow operators to configure minimum and maximum concurrency limits per worker type.

#### Scenario: Minimum concurrency enforcement

- **WHEN** an operator configures a minimum concurrency (e.g., 2)
- **THEN** the system never reduces concurrency below that minimum
- **AND** validates that minimum is at least 1 and at most equal to maximum

#### Scenario: Maximum concurrency enforcement

- **WHEN** an operator configures a maximum concurrency (e.g., 10)
- **THEN** the system never increases concurrency above that maximum
- **AND** validates that maximum is greater than or equal to minimum

#### Scenario: Default concurrency bounds

- **WHEN** a worker starts without explicit min/max configuration
- **THEN** the system uses default minimum of 1
- **AND** uses default maximum of 10
- **AND** uses these defaults until explicitly overridden

### Requirement: Per-Worker Adaptive Scaling Toggle

The system SHALL allow operators to enable or disable adaptive scaling independently for each worker type.

#### Scenario: Adaptive scaling disabled by default

- **WHEN** a worker starts without explicit adaptive scaling configuration
- **THEN** adaptive scaling is disabled
- **AND** the worker uses its configured static concurrency (legacy behavior)
- **AND** no health-based adjustments are made

#### Scenario: Enabling adaptive scaling via API

- **WHEN** an operator enables adaptive scaling for a worker via the config API
- **THEN** the worker begins monitoring system health on the next polling cycle
- **AND** adjusts concurrency according to health score and configured bounds
- **AND** the change takes effect without requiring a restart

#### Scenario: Disabling adaptive scaling via API

- **WHEN** an operator disables adaptive scaling for a worker via the config API
- **THEN** the worker reverts to its configured static concurrency
- **AND** health monitoring continues but no longer affects this worker's concurrency
- **AND** the change takes effect on the next polling cycle

### Requirement: Health-Aware Job Polling

The system SHALL integrate health monitoring into the worker polling loop to make concurrency decisions before fetching jobs.

#### Scenario: Pre-fetch health check

- **WHEN** a worker enters its job polling cycle
- **THEN** the system queries the current health score before fetching jobs
- **AND** adjusts the active concurrency limit if needed
- **AND** fetches jobs only up to the current concurrency limit

#### Scenario: Concurrency enforcement with semaphore

- **WHEN** processing jobs with adaptive concurrency enabled
- **THEN** the system maintains a semaphore with capacity equal to current concurrency
- **AND** updates the semaphore capacity when concurrency is adjusted
- **AND** blocks new job starts if concurrency limit is reached

#### Scenario: In-flight job completion

- **WHEN** concurrency is reduced while jobs are in flight
- **THEN** the system allows currently running jobs to complete
- **AND** does not start new jobs until running count drops below new limit
- **AND** logs any jobs that are delayed due to concurrency reduction

### Requirement: Concurrency Adjustment Logging

The system SHALL log all concurrency adjustments with context for debugging and audit purposes.

#### Scenario: Concurrency change logging

- **WHEN** worker concurrency is adjusted
- **THEN** the system logs at INFO level:
  - Worker type
  - Previous concurrency value
  - New concurrency value
  - Current health score
  - Triggering health zone (critical/warning/safe)
  - Timestamp

#### Scenario: Adjustment rate limiting

- **WHEN** health score fluctuates rapidly (multiple changes within cooldown period)
- **THEN** the system logs each health score change at DEBUG level
- **AND** logs concurrency adjustments only when they actually occur (after cooldown)
- **AND** includes a note that rapid fluctuations are being dampened

### Requirement: Prometheus Metrics for Concurrency

The system SHALL expose adaptive concurrency metrics via Prometheus for monitoring and alerting.

#### Scenario: Current concurrency gauge

- **WHEN** worker concurrency is adjusted
- **THEN** the system updates a `worker_current_concurrency` gauge metric
- **AND** labels the metric with worker type (e.g., `worker_type="chunk_embedding"`)

#### Scenario: Target vs actual concurrency

- **WHEN** concurrency is limited by health score
- **THEN** the system exposes separate gauges:
  - `worker_target_concurrency` (based on health score)
  - `worker_actual_concurrency` (accounting for in-flight job limits)

#### Scenario: Concurrency adjustment events

- **WHEN** concurrency is adjusted
- **THEN** the system increments a `worker_concurrency_adjustments_total` counter
- **AND** labels the counter with:
  - `worker_type` (e.g., "chunk_embedding")
  - `direction` ("increase" or "decrease")
  - `reason` ("health_critical", "health_warning", "health_safe")

#### Scenario: Throttled job counter

- **WHEN** jobs are delayed due to concurrency limits
- **THEN** the system increments a `worker_jobs_throttled_total` counter
- **AND** labels the counter with worker type

### Requirement: Scaling Configuration API Extension

The system SHALL extend the existing runtime configuration API to support adaptive scaling parameters.

#### Scenario: Query current adaptive configuration

- **WHEN** an operator queries the worker configuration endpoint
- **THEN** the response includes:
  - `enable_adaptive_scaling` (boolean)
  - `min_concurrency` (integer)
  - `max_concurrency` (integer)
  - `current_concurrency` (integer, current effective value)
  - `health_score` (integer 0-100, latest known score)

#### Scenario: Update adaptive scaling settings

- **WHEN** an operator updates adaptive scaling settings via the API
- **THEN** the system validates:
  - `min_concurrency >= 1`
  - `max_concurrency >= min_concurrency`
  - `max_concurrency <= 50` (safety limit to prevent runaway scaling)
- **AND** applies the new settings on the next polling cycle
- **AND** returns the updated configuration in the response

#### Scenario: Backward compatibility with legacy config

- **WHEN** an operator updates only the legacy `worker_concurrency` field
- **THEN** the system treats it as `max_concurrency` if adaptive scaling is enabled
- **AND** uses it as static concurrency if adaptive scaling is disabled
- **AND** logs a deprecation warning recommending explicit adaptive scaling fields

### Requirement: Cooldown Period Configuration

The system SHALL enforce configurable cooldown periods to prevent rapid concurrency oscillations.

#### Scenario: Default cooldown period

- **WHEN** adaptive scaling is enabled without explicit cooldown configuration
- **THEN** the system uses a 5-minute cooldown for concurrency increases
- **AND** uses a 1-minute cooldown for concurrency decreases (faster response to pressure)

#### Scenario: Custom cooldown configuration

- **WHEN** an operator configures custom cooldown periods
- **THEN** the system validates that cooldown is at least 30 seconds
- **AND** applies the configured cooldown to future adjustments

#### Scenario: Cooldown bypass for critical health

- **WHEN** system health enters the critical zone (0-33)
- **THEN** the system bypasses the cooldown period
- **AND** immediately reduces concurrency to minimum
- **AND** logs the cooldown bypass with reason

### Requirement: Graceful Handling of Health Monitor Unavailability

The system SHALL handle scenarios where the health monitor is temporarily unavailable or unresponsive.

#### Scenario: Health monitor timeout

- **WHEN** a worker queries health score but the monitor doesn't respond within 5 seconds
- **THEN** the system uses the last known health score
- **AND** logs a WARNING about health monitor unavailability
- **AND** continues processing jobs at current concurrency

#### Scenario: Stale health data

- **WHEN** the last health score is older than 2 minutes
- **THEN** the system assumes a moderate health score (50)
- **AND** adjusts concurrency to 50% of maximum as a safe default
- **AND** logs a WARNING about stale health data

#### Scenario: Health monitor recovery

- **WHEN** the health monitor becomes available after being unavailable
- **THEN** the system resumes normal adaptive scaling
- **AND** logs the recovery at INFO level
- **AND** respects cooldown periods for the next adjustment

### Requirement: Safety Limits and Circuit Breaker

The system SHALL enforce safety limits to prevent extreme scaling behavior.

#### Scenario: Maximum concurrency cap

- **WHEN** calculating target concurrency based on health score
- **THEN** the system enforces an absolute maximum of 50 concurrent jobs per worker type
- **AND** logs an ERROR if configuration attempts to exceed this limit

#### Scenario: Minimum concurrency floor

- **WHEN** health score is critical and would reduce concurrency below 1
- **THEN** the system maintains at least 1 concurrent job
- **AND** logs that minimum concurrency floor was enforced

#### Scenario: Concurrency change rate limit

- **WHEN** adjusting concurrency upward
- **THEN** the system limits increases to 50% of current value per adjustment cycle
- **AND** prevents more than one upward adjustment within the cooldown period

#### Scenario: Circuit breaker for repeated failures

- **WHEN** a worker experiences high failure rates (>50% jobs failed in last 10 jobs)
- **THEN** the system reduces concurrency to minimum regardless of health score
- **AND** logs a CRITICAL alert about the failure rate
- **AND** disables further increases until failure rate drops below 25%
