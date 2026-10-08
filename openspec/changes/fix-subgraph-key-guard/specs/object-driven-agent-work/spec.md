## MODIFIED Requirements

### Requirement: Correct, crash-safe claim

Claiming SHALL transition the object from the ready status to the in-progress status under the object's advisory lock and the versioned write model; a lost race SHALL leave the run skipped without consuming the failure budget.

#### Scenario: Claim is serialized

- **WHEN** two runs are dispatched for the same object
- **THEN** only one transition to in-progress succeeds and the other run is skipped

#### Scenario: Skipped claim does not burn budget

- **WHEN** a run's claim finds the object already taken
- **THEN** the run is marked skipped, the job completes, and the item's failure budget is unchanged

#### Scenario: Board-enabled types require a stable key

- **WHEN** a board-enabled work object is created without a key
- **THEN** the write is rejected, because the claim requires a stable identity

#### Scenario: Subgraph creation rejects a keyless board-enabled object

- **WHEN** a board-enabled work object is created via the subgraph/bulk path (`POST /api/graph/subgraph`) without a key
- **THEN** the request is rejected with `work_status_key_required` and no object or relationship is created
