# entity-extraction Delta Specification

## Purpose

Extends extraction worker configuration to support adaptive scaling based on system health, enabling dynamic concurrency adjustments to prevent resource exhaustion while maintaining job throughput.

## ADDED Requirements

### Requirement: Adaptive Scaling Configuration

The system SHALL allow operators to enable adaptive concurrency scaling for extraction workers.

#### Scenario: Enable adaptive scaling for a worker

- **WHEN** an operator configures `enable_adaptive_scaling: true` for an extraction worker
- **THEN** the worker dynamically adjusts its concurrency based on system health score
- **AND** respects the configured `min_concurrency` and `max_concurrency` bounds
- **AND** uses health-aware polling to prevent resource exhaustion

#### Scenario: Disable adaptive scaling for a worker

- **WHEN** an operator configures `enable_adaptive_scaling: false` for an extraction worker
- **THEN** the worker uses static concurrency defined by `worker_concurrency`
- **AND** ignores system health scores when fetching jobs
- **AND** maintains legacy behavior for backward compatibility

#### Scenario: Default adaptive scaling behavior

- **WHEN** an extraction worker starts without explicit adaptive scaling configuration
- **THEN** `enable_adaptive_scaling` defaults to `false`
- **AND** the worker operates with legacy static concurrency

### Requirement: Concurrency Bounds Configuration

The system SHALL allow operators to define minimum and maximum concurrency limits for extraction workers with adaptive scaling enabled.

#### Scenario: Configure minimum concurrency

- **WHEN** an operator sets `min_concurrency` for a worker
- **THEN** the system validates that `min_concurrency >= 1`
- **AND** ensures the worker never processes fewer than `min_concurrency` jobs concurrently
- **AND** uses this as the floor during critical health conditions

#### Scenario: Configure maximum concurrency

- **WHEN** an operator sets `max_concurrency` for a worker
- **THEN** the system validates that `max_concurrency >= min_concurrency`
- **AND** validates that `max_concurrency <= 50` (safety limit)
- **AND** ensures the worker never processes more than `max_concurrency` jobs concurrently
- **AND** uses this as the ceiling during safe health conditions

#### Scenario: Default concurrency bounds

- **WHEN** a worker has adaptive scaling enabled but no explicit min/max configuration
- **THEN** `min_concurrency` defaults to 1
- **AND** `max_concurrency` defaults to 10
- **AND** these defaults apply until explicitly overridden

### Requirement: Worker Configuration API Extension

The system SHALL extend the existing worker configuration API to support adaptive scaling parameters.

#### Scenario: Retrieve worker configuration with adaptive scaling

- **WHEN** an operator queries `GET /admin/extraction/embedding/config`
- **THEN** the response includes:
  - `worker_concurrency` (integer, legacy field for backward compatibility)
  - `enable_adaptive_scaling` (boolean, default: false)
  - `min_concurrency` (integer, default: 1)
  - `max_concurrency` (integer, default: 10)
  - `current_concurrency` (integer, current effective concurrency)
  - `health_score` (integer 0-100, latest system health score)

#### Scenario: Update worker configuration with adaptive scaling

- **WHEN** an operator posts to `POST /admin/extraction/embedding/config` with adaptive scaling fields
- **THEN** the system validates all constraints:
  - `min_concurrency >= 1`
  - `max_concurrency >= min_concurrency`
  - `max_concurrency <= 50`
- **AND** applies the configuration on the next worker polling cycle
- **AND** returns the updated configuration in the response
- **AND** logs the configuration change with operator identity

#### Scenario: Backward compatibility with legacy API

- **WHEN** an operator updates only `worker_concurrency` via the API
- **THEN** the system treats it as static concurrency if adaptive scaling is disabled
- **AND** treats it as `max_concurrency` if adaptive scaling is enabled
- **AND** logs a deprecation warning recommending explicit adaptive scaling fields

### Requirement: Health-Aware Job Polling Integration

The system SHALL integrate health monitoring into extraction worker polling loops to enable dynamic concurrency.

#### Scenario: Health check before job fetch

- **WHEN** an extraction worker with adaptive scaling enabled enters its polling cycle
- **THEN** the worker queries the system health score before fetching jobs
- **AND** adjusts its concurrency limit based on the health score and configured bounds
- **AND** fetches only up to the adjusted concurrency limit

#### Scenario: Semaphore-based concurrency control

- **WHEN** processing extraction jobs with adaptive concurrency
- **THEN** the worker maintains a buffered channel semaphore with capacity equal to current concurrency
- **AND** updates the semaphore capacity when concurrency is adjusted
- **AND** blocks new job starts if the semaphore is full

#### Scenario: In-flight job handling during concurrency reduction

- **WHEN** concurrency is reduced while extraction jobs are in flight
- **THEN** the worker allows currently running jobs to complete normally
- **AND** waits for running jobs to finish before starting new jobs if over the new limit
- **AND** logs delayed jobs due to health-based throttling

### Requirement: Worker-Specific Adaptive Scaling Metrics

The system SHALL expose Prometheus metrics for adaptive scaling behavior per extraction worker type.

#### Scenario: Current concurrency metrics

- **WHEN** an extraction worker's concurrency is adjusted
- **THEN** the system updates a `extraction_worker_current_concurrency` gauge
- **AND** labels it with `worker_type` (e.g., "graph_embedding", "chunk_embedding", "document_parsing", "object_extraction")

#### Scenario: Concurrency adjustment events

- **WHEN** a worker's concurrency changes due to health score
- **THEN** the system increments a `extraction_worker_concurrency_adjustments_total` counter
- **AND** labels it with:
  - `worker_type`
  - `direction` ("increase" or "decrease")
  - `reason` ("health_critical", "health_warning", "health_safe")

#### Scenario: Throttled extraction jobs

- **WHEN** extraction jobs are delayed due to concurrency limits
- **THEN** the system increments a `extraction_jobs_throttled_total` counter
- **AND** labels it with `worker_type`

### Requirement: Gradual Rollout Support

The system SHALL support enabling adaptive scaling gradually across different extraction worker types.

#### Scenario: Enable adaptive scaling for ChunkEmbedding worker only

- **WHEN** an operator enables adaptive scaling for the ChunkEmbedding worker
- **THEN** only ChunkEmbedding jobs use health-aware concurrency
- **AND** other workers (GraphEmbedding, DocumentParsing, ObjectExtraction) continue with static concurrency
- **AND** the system logs which workers have adaptive scaling enabled

#### Scenario: Phased rollout across worker types

- **WHEN** an operator incrementally enables adaptive scaling for additional worker types
- **THEN** each worker independently adjusts concurrency based on health
- **AND** configuration changes apply on the next polling cycle without restart
- **AND** the system maintains separate metrics per worker type

#### Scenario: Emergency rollback for a worker type

- **WHEN** an operator disables adaptive scaling for a specific worker type
- **THEN** that worker immediately reverts to its configured static concurrency
- **AND** other workers with adaptive scaling remain unaffected
- **AND** the change takes effect within one polling cycle

### Requirement: Extraction Worker Logging for Adaptive Scaling

The system SHALL log adaptive scaling behavior for extraction workers with context for debugging.

#### Scenario: Concurrency adjustment logging

- **WHEN** an extraction worker's concurrency is adjusted
- **THEN** the system logs at INFO level:
  - Worker type (e.g., "GraphEmbedding")
  - Previous concurrency
  - New concurrency
  - Current health score
  - Triggering health zone (critical/warning/safe)

#### Scenario: Health-based job throttling logging

- **WHEN** extraction jobs are delayed due to health-based concurrency limits
- **THEN** the system logs at DEBUG level:
  - Worker type
  - Number of jobs waiting
  - Current concurrency limit
  - Current health score

#### Scenario: Configuration change logging

- **WHEN** an operator updates adaptive scaling configuration for a worker
- **THEN** the system logs at INFO level:
  - Worker type
  - Changed fields (enable_adaptive_scaling, min_concurrency, max_concurrency)
  - Previous and new values
  - Operator identity (if available)
