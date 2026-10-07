## MODIFIED Requirements

### Requirement: Derive a governor mode from system health

The governor SHALL derive a `normal` / `throttle` / `shed` mode from the system health monitor and attributable capacity. Escalation SHALL move to `throttle` when host I/O wait reaches the throttle threshold or load reaches the warning factor. Escalation to `shed` SHALL occur only when database pool utilization reaches the shed threshold **and** in-flight bulk admissions reach the shed bound for a configured number of consecutive samples. Host I/O/CPU pressure SHALL drive `throttle` only and SHALL NOT cause shedding. De-escalation back to `normal` SHALL require a configured number of consecutive healthy samples. Mode transitions SHALL only advance on a new health sample (a newer timestamp), and stale health SHALL NOT cause shedding.

#### Scenario: Healthy stays normal

- **WHEN** the health monitor reports healthy samples
- **THEN** the governor mode is `normal`

#### Scenario: Sustained high I/O wait escalates

- **WHEN** I/O wait rises above the throttle threshold for the configured number of consecutive samples
- **THEN** the governor escalates `normal` → `throttle` (host pressure never drives `shed`)

#### Scenario: DB pool and bulk inflight escalate to shed

- **WHEN** database pool utilization is at or above the shed threshold and at least the shed-bound number of bulk admissions are in flight for the configured number of consecutive samples
- **THEN** the governor mode escalates `normal` → `shed`

#### Scenario: Host pressure throttles but never sheds

- **WHEN** host I/O wait or CPU load rises above the throttle thresholds
- **THEN** the governor mode is `throttle`
- **AND** the governor never escalates to `shed` from host pressure alone

#### Scenario: Stale health does not shed

- **WHEN** health metrics are stale
- **THEN** the governor does not escalate to `shed`

### Requirement: Shed heavy work and bound concurrency

The governor SHALL reject bulk-class admissions with a `shed` signal when in `shed` mode, and SHALL reject admissions with a `no-capacity` signal when per-project or class-segregated global inflight limits, or the interactive per-project rate limit, are exhausted. Bulk-class admissions SHALL be exempt from the interactive per-project rate limit (bulk is governed by inflight + backpressure, optionally a separate bulk rate). Interactive-class admissions SHALL NOT be shed.

#### Scenario: Bulk shed, interactive admitted

- **WHEN** the governor is in `shed` mode
- **THEN** a bulk-class admission is rejected
- **AND** an interactive-class admission is still admitted

#### Scenario: Per-project limit exhausted

- **WHEN** a project already holds its inflight slot
- **THEN** a further admission for that project is rejected with no capacity

#### Scenario: Bulk budget does not starve interactive

- **WHEN** the bulk global inflight budget is exhausted
- **THEN** an interactive admission is still admitted (class-segregated budgets)

### Requirement: Pause background work under load

Background embedding workers SHALL skip work while the governor is throttled or shedding, and SHALL be paused when the governor enters `throttle` or `shed` and resumed when it returns to `normal`.

#### Scenario: Sweep skips under load

- **WHEN** the governor is throttled or shedding
- **THEN** the embedding sweep worker skips its sweep

#### Scenario: Workers pause on shed

- **WHEN** the governor transitions into `throttle` or `shed`
- **THEN** the object, relationship, and sweep embedding workers are paused
- **AND** they are resumed when the governor returns to `normal`

## ADDED Requirements

### Requirement: Bound bulk work with backpressure

Under `throttle`, bulk admission SHALL wait up to a configured budget for capacity before returning HTTP 429 with a `Retry-After` header. The number of concurrent waiters SHALL be bounded; excess waiters SHALL fail fast with no capacity. Bulk admission SHALL respect request cancellation. Bulk work SHALL NOT be shed in `throttle`.

#### Scenario: Bulk waits then is rejected on budget expiry

- **WHEN** the governor is throttling and a bulk admission cannot acquire capacity within the wait budget
- **THEN** the server responds 429 with a `Retry-After` header (no shed)

#### Scenario: Bulk admission cancelled while waiting

- **WHEN** a bulk admission is waiting for capacity and the request context is cancelled
- **THEN** the admission returns the context error and releases everything it acquired

#### Scenario: Waiter bound exceeded

- **WHEN** the number of concurrent bulk waiters reaches the configured bound
- **THEN** a further bulk admission fails fast with no capacity

### Requirement: Surface governor state in health

The `/health` endpoint SHALL expose the resource governor's current mode, database pool utilization, and in-flight bulk admissions as informational fields. These fields SHALL NOT affect the overall health status or HTTP status code.

#### Scenario: Governor state present

- **WHEN** a client reads `/health`
- **THEN** the response includes the governor's `mode`, `db_pool_percent`, and `bulk_inflight` fields

#### Scenario: Governor state does not change status

- **WHEN** the governor is in `shed` mode
- **THEN** the overall health status and HTTP status code are unchanged (the governor state is informational only)
