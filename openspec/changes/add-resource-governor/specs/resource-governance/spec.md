## Purpose

Prevent a saturated host (high I/O wait or CPU load) from being driven further into failure by heavy, background work. The governor reduces demand — shedding bulk work early with HTTP 429 + `Retry-After` and pausing background embedding workers — rather than adding capacity.

## ADDED Requirements

### Requirement: Derive a governor mode from system health

The governor SHALL derive a `normal` / `throttle` / `shed` mode from the system health monitor. Escalation SHALL move to `throttle` when I/O wait reaches the throttle threshold or load reaches the warning factor, and to `shed` when I/O wait reaches the shed threshold or load reaches the shed factor for a configured number of consecutive samples. De-escalation back to `normal` SHALL require a configured number of consecutive healthy samples. Mode transitions SHALL only advance on a new health sample (a newer timestamp), and stale health SHALL NOT cause shedding.

#### Scenario: Healthy stays normal

- **WHEN** the health monitor reports healthy samples
- **THEN** the governor mode is `normal`

#### Scenario: Sustained high I/O wait escalates

- **WHEN** I/O wait rises above the throttle threshold and then above the shed threshold for the configured number of consecutive samples
- **THEN** the governor mode escalates `normal` → `throttle` → `shed`

#### Scenario: Stale health does not shed

- **WHEN** health metrics are stale
- **THEN** the governor does not escalate to `shed`

### Requirement: Shed heavy work and bound concurrency

The governor SHALL reject bulk-class admissions with a `shed` signal when in `shed` mode, and SHALL reject admissions with a `no-capacity` signal when per-project or global inflight limits, or the per-project rate limit, are exhausted. Interactive-class admissions SHALL NOT be shed.

#### Scenario: Bulk shed, interactive admitted

- **WHEN** the governor is in `shed` mode
- **THEN** a bulk-class admission is rejected
- **AND** an interactive-class admission is still admitted

#### Scenario: Per-project limit exhausted

- **WHEN** a project already holds its inflight slot
- **THEN** a further admission for that project is rejected with no capacity

### Requirement: Reject with HTTP 429 and Retry-After

The bulk HTTP middleware SHALL return HTTP 429 with a `Retry-After` header and an apperror-shaped JSON body when admission is rejected.

#### Scenario: Bulk endpoint under load

- **WHEN** a bulk endpoint is hit while the governor is shedding
- **THEN** the server responds 429 with a `Retry-After` header

### Requirement: Pause background work under load

Background embedding workers SHALL skip work while the governor is throttled or shedding, and SHALL be paused when the governor enters `shed` and resumed when it recovers.

#### Scenario: Sweep skips under load

- **WHEN** the governor is throttled or shedding
- **THEN** the embedding sweep worker skips its sweep

#### Scenario: Workers pause on shed

- **WHEN** the governor transitions into `shed`
- **THEN** the object, relationship, and sweep embedding workers are paused
- **AND** they are resumed when the governor leaves `shed`
