## MODIFIED Requirements

### Requirement: Board-enabled types declare and validate their work status

An object type SHALL be able to be flagged board-enabled; a board-enabled type SHALL declare its allowed work status values, and a write outside that set SHALL be rejected at every generic write entry point — the single-object create, create-or-update, and patch doors, the bulk status update, and the subgraph/bulk create door. A batch write that carries one off-vocabulary board-enabled object SHALL be rejected atomically, before any row is written. A non-board or unconfigured type remains unconstrained.

#### Scenario: Invalid status rejected

- **WHEN** a board-enabled type allowing only `ready`, `in_progress`, `review`, `done` is written with `status = "shipped"`
- **THEN** the write is rejected and the object is unchanged

#### Scenario: Non-board type unconstrained

- **WHEN** an object of a type that is not board-enabled is written with any status string within the field limit
- **THEN** the write is accepted

#### Scenario: Subgraph create rejects an off-vocabulary status atomically

- **WHEN** a subgraph create carries a board-enabled object whose status is outside the type's declared allowed set
- **THEN** the whole request is rejected before the transaction and no object is created

### Requirement: A single writer owns work status

For board-enabled types, `status` SHALL be authoritative and SHALL be changed only through the platform's work path, which creates the next version and keeps `properties["status"]` consistent. A direct agent write that sets work status SHALL be rejected — covering **both** the `status` field **and** `properties["status"]`, at every write entry point (create, create-or-update, patch, bulk status update, and subgraph/bulk create). Every work-path transition SHALL record an explicit `change_summary` on the new version so board history reads a recorded change rather than inferring one: the status move as an updated `/status` path (previous → new), and the assignee move — a built-in column — as a synthetic `/assignee` path. A transition that changes neither the status nor the assignee SHALL record no summary.

#### Scenario: Agent cannot set status directly

- **WHEN** an agent attempts to write `status` on a board-enabled work object through a direct graph write
- **THEN** the write is rejected and the object is unchanged

#### Scenario: Agent cannot smuggle status through properties

- **WHEN** an agent attempts to write `properties["status"]` on a board-enabled work object
- **THEN** the write is rejected and the object is unchanged

#### Scenario: Platform transition keeps both copies consistent

- **WHEN** the platform transitions a board-enabled object's status
- **THEN** the new version's `status` column and `properties["status"]` agree

#### Scenario: Work-path transition records an explicit change summary

- **WHEN** the platform transitions a board-enabled object's status
- **THEN** the new version's `change_summary` records the `/status` move (previous → new) and, when the assignee also moved, the `/assignee` move

#### Scenario: No-op transition records no change

- **WHEN** a work-path transition leaves both the status and the assignee unchanged
- **THEN** the new version's `change_summary` is empty
