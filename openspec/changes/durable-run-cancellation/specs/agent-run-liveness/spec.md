## MODIFIED Requirements

### Requirement: Explicit cancel stops an in-flight detached run by id

The executor SHALL track in-flight runs by run id and expose an explicit cancel by run id as a **fast-path notification** only. The agent-run cancel endpoint SHALL apply the cancel as a **durable guarded transition** of the persisted run row: a non-terminal run is moved to the intermediate `cancelling` status, a write authoritative in Postgres and visible to every server instance. The endpoint SHALL base its response on whether that transition changed a row. The executing instance — in whichever process it runs — SHALL observe its own persisted `cancelling` status at step boundaries and stop, then take the guarded terminal transition to `cancelled` with the reached step count and the user-cancel reason — never a `context canceled` server fault. The registry entry SHALL be removed when the run finishes, and a stale removal SHALL NOT evict a newer run that reused the id.

#### Scenario: Explicit cancel stops a detached run

- **WHEN** an explicit cancel is issued for an in-flight detached run
- **THEN** the run row is moved to the durable `cancelling` intent, the executor's registered cancel context is cancelled, and the run finishes with status `cancelled` and `error_message` equal to the user-cancel reason

#### Scenario: Cancel reason is honest

- **WHEN** a run stops because of an explicit cancel
- **THEN** its terminal status is `cancelled`, not `failed`, and no `context canceled` server-fault message is recorded

#### Scenario: Cancel of a non-in-flight run uses the guarded row transition

- **WHEN** an explicit cancel is issued for a run that is not currently executing in this process
- **THEN** the guarded `cancelling` transition is still applied (it succeeds for a non-terminal row), the executor reports no in-flight match, and the endpoint reports `cancelled:true` with the committed status

#### Scenario: Repeat cancel of an already-cancelling run stays accepted

- **WHEN** a cancel is issued for a run already in the `cancelling` status
- **THEN** the guarded transition matches the row again and the endpoint keeps reporting `cancelled:true`

### Requirement: Terminal transitions are race-safe and authoritative

Every terminal run transition (success, failure, skipped, cancelled) SHALL be guarded to rows that have not yet reached a terminal state, so competing terminal writers cannot overwrite one another. A run parked in the intermediate `cancelling` status SHALL be finalizable only as `cancelled`: a success, skip, or failure writer that races a committed cancel SHALL resolve the row to `cancelled` **inside the same atomic UPDATE** (a `CASE` over the pre-UPDATE status), so a committed cancel can never be turned into `completed` or `failed` even if it lands between the executor's status read and its write. The cancel endpoint SHALL report whether it committed the cancel, and its response SHALL match the run's terminal status: a cancel that arrives after the run already finished SHALL leave the row untouched and SHALL be reported as not cancelled.

#### Scenario: Cancel lands before the completion write

- **WHEN** a cancel commits the run to `cancelling` and the executor's success write then races it
- **THEN** the success write resolves the run to `cancelled` and the terminal status remains `cancelled`, matching the endpoint's response

#### Scenario: Cancel lands after the completion write

- **WHEN** a run already completed and a cancel is then issued
- **THEN** the guarded cancel changes no rows, the run stays `completed`, and the endpoint reports `cancelled: false` with the terminal status

#### Scenario: Concurrent cancel and complete

- **WHEN** a cancel and a completion execute concurrently against the same run
- **THEN** exactly one terminal transition is applied and the final status is `cancelled` if and only if the cancel reported that it committed

## ADDED Requirements

### Requirement: Cancellation is durable and cross-instance

A cancel request SHALL be recorded as a durable status change in Postgres, so that a cancel served by one server instance can stop a run executing in a different instance whose in-process cancel registry does not know about the run. The executing instance SHALL observe its own persisted status at pipeline start, at each step boundary, and before its terminal write, and SHALL stop and finalize as `cancelled` when the status is `cancelling`. Correctness SHALL NOT depend on the per-process cancel registry. A run left in `cancelling` with no live executor SHALL be resolved to `cancelled` — never to a failure — by startup recovery and by the stale-run reaper.

#### Scenario: Cancel on one instance stops a run on another

- **WHEN** an instance with no in-flight run for a run id commits a durable cancel, and that run is executing in a different instance with no local notification
- **THEN** the executing instance observes the persisted `cancelling` status at its next step boundary, stops, and finalizes the run as `cancelled`

#### Scenario: A committed cancel is never reported as success

- **WHEN** the executing instance reaches its terminal write after a durable cancel was committed, without having observed it at a step boundary
- **THEN** the terminal write finalizes the run as `cancelled`, not `completed` or `failed`

#### Scenario: Orphaned cancelling run is finalized as cancelled

- **WHEN** a run is in `cancelling` status and no executing instance observes it (the process died, or a queued run was never claimed)
- **THEN** startup recovery (for every `cancelling` row) or the stale-run reaper (for rows idle past the threshold) finalizes it as `cancelled`, never as `failed`
