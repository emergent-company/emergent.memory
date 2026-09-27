## ADDED Requirements

### Requirement: Run lifetime can be detached from the triggering request

The executor SHALL expose a way to run an agent on a context that carries the triggering request's values but not its cancellation. A surface that starts a run whose lifetime must outlive its HTTP request (the chat SSE surface) SHALL use it, so cancelling the request context detaches the stream consumer instead of aborting the run. The run SHALL remain bounded by the existing per-step watchdog and SHALL continue to persist its run record and messages.

#### Scenario: Request cancellation does not abort a detached run

- **WHEN** a chat agent run is started on the detached context and the SSE request context is cancelled while the run is in flight
- **THEN** the run completes with status `success` and its run record is persisted as `success`

#### Scenario: Request-bound runs still abort

- **WHEN** a run is executed directly on a request context (not detached) and that context is cancelled mid-run
- **THEN** the run terminates with status `failed` (the pre-fix binding, retained for non-detached surfaces)

### Requirement: Explicit cancel stops an in-flight detached run by id

The executor SHALL track in-flight runs by run id and expose an explicit cancel by run id. The agent-run cancel endpoint SHALL apply the cancel as a guarded terminal transition of the persisted run row, then route a best-effort stop through the executor so an in-flight, detached run winds down promptly. An explicit stop SHALL be recorded as terminal status `cancelled` with the reached step count and the user-cancel reason — never as a `context canceled` server fault. The registry entry SHALL be removed when the run finishes, and a stale removal SHALL NOT evict a newer run that reused the id.

#### Scenario: Explicit cancel stops a detached run

- **WHEN** an explicit cancel is issued for an in-flight detached run
- **THEN** the run row is transitioned to `cancelled` with `error_message` equal to the user-cancel reason, the executor's registered cancel context is cancelled, and the run finishes with status `cancelled`

#### Scenario: Cancel reason is honest

- **WHEN** a run stops because of an explicit cancel
- **THEN** its terminal status is `cancelled`, not `failed`, and no `context canceled` server-fault message is recorded

#### Scenario: Cancel of a non-in-flight run uses the guarded row transition

- **WHEN** an explicit cancel is issued for a run that is not currently executing in this process
- **THEN** the guarded row transition is applied (it succeeds for a non-terminal row) and the executor reports no in-flight match; the endpoint response reflects whether the row actually transitioned

### Requirement: Terminal transitions are race-safe and authoritative

Every terminal run transition (success, failure, skipped, cancelled) SHALL be guarded to rows that have not yet reached a terminal state, so competing terminal writers cannot overwrite one another. The first writer wins and the later writer is a no-op. The cancel endpoint SHALL report whether it actually transitioned the run, and its response SHALL match the run's terminal status: a cancel that arrives after the run already finished SHALL leave the row untouched and SHALL be reported as not cancelled. The endpoint SHALL NOT clobber a completed run into `cancelled`, nor SHALL a late completion turn a `cancelled` run into `success`.

#### Scenario: Cancel lands before the completion write

- **WHEN** a cancel transitions the run to `cancelled` and the executor's success write then races it
- **THEN** the success write is a no-op and the run's terminal status remains `cancelled`, matching the endpoint's response

#### Scenario: Cancel lands after the completion write

- **WHEN** a run already completed and a cancel is then issued
- **THEN** the guarded cancel changes no rows, the run stays `completed`, and the endpoint reports `cancelled: false` with the terminal status

#### Scenario: Concurrent cancel and complete

- **WHEN** a cancel and a completion execute concurrently against the same run
- **THEN** exactly one terminal transition is applied and the final status is `cancelled` if and only if the cancel reported that it transitioned the row

