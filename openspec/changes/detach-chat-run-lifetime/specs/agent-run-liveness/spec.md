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

The executor SHALL track in-flight runs by run id and expose an explicit cancel by run id. The agent-run cancel endpoint SHALL route the cancel through the executor before falling back to updating the persisted run row, so a run whose lifetime no longer has the request context can still be stopped. An explicit stop SHALL be recorded as terminal status `cancelled` with the reached step count and the user-cancel reason — never as a `context canceled` server fault. The registry entry SHALL be removed when the run finishes, and a stale removal SHALL NOT evict a newer run that reused the id.

#### Scenario: Explicit cancel stops a detached run

- **WHEN** an explicit cancel is issued for an in-flight detached run
- **THEN** the executor's registered cancel context is cancelled and the run finishes with status `cancelled` and `error_message` equal to the user-cancel reason

#### Scenario: Cancel reason is honest

- **WHEN** a run stops because of an explicit cancel
- **THEN** its terminal status is `cancelled`, not `failed`, and no `context canceled` server-fault message is recorded

#### Scenario: Cancel falls back for a non-in-flight run

- **WHEN** an explicit cancel is issued for a run that is not currently executing in this process
- **THEN** the endpoint updates the persisted run row to `cancelled` (existing behaviour) and the executor reports no in-flight match
