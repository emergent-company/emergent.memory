## MODIFIED Requirements

### Requirement: Kanban projection

The board SHALL be a read projection over board-enabled object types joined to their runs, with no separate work-item store. The board SHALL derive its draggable transitions and its work-item actions from the project's work-path status mapping — the object status value each lifecycle phase resolves to, declared on the project's agents' `workConfig.status` — instead of the built-in status literals. It SHALL fall back to the built-in statuses (`ready`, `in_progress`, `review`, `revision`, `blocked`, `done`) when no mapping is declared. A phase SHALL be remapped only when every agent declaring it agrees on a single non-empty value; an unset or ambiguous phase keeps its built-in default, so a board with no configured mapping is unchanged. The supported transitions are `blocked → ready` (execute/retry), `review → done` (approve), `review → revision` (open the request-changes form), and any non-`done → blocked` (cancel), resolved through the mapping; any other pair is rejected in the UI. Activating a card SHALL open the shared read-only object preview rather than a board-specific summary.

#### Scenario: Columns from work status

- **WHEN** board-enabled objects exist with different statuses
- **THEN** they are grouped into columns by status

#### Scenario: Execution badge

- **WHEN** an object has associated runs
- **THEN** the latest run's execution status is shown on its card

#### Scenario: Dragging executes

- **WHEN** a card is moved from the blocked lane to the ready lane
- **THEN** the object's status is updated through the work path and a run is enqueued

#### Scenario: Dragging approves a review

- **WHEN** a card in the review lane is moved to the done lane
- **THEN** the item is approved through the work path

#### Scenario: Dragging cancels a non-done card

- **WHEN** a card that is not done is moved to the blocked lane
- **THEN** the item is cancelled through the work path

#### Scenario: Unsupported move is rejected in the UI

- **WHEN** a card is dragged to a lane that is not one of the supported transitions for its current status
- **THEN** no request is issued and the drop is rejected

#### Scenario: Revision drag requests feedback

- **WHEN** a card in the review lane is moved to the revision lane
- **THEN** the item's request-changes form is opened so feedback can be supplied, rather than a blind transition

#### Scenario: Custom-mapped transitions use the configured statuses

- **WHEN** the project's work-path mapping resolves ready to `todo` and blocked to `rejected`
- **THEN** dragging a `rejected` card to the `todo` lane fires the retry/execute action, and a literal `blocked` card is not a valid source for that transition

#### Scenario: Custom-mapped statuses gate the drawer actions

- **WHEN** the work-path mapping resolves review to `checking`, blocked to `rejected`, and done to `shipped`
- **THEN** a `checking` item exposes approve and request-changes, a `rejected` item exposes retry, and a `shipped` item exposes neither cancel nor reassign

#### Scenario: An ambiguous phase keeps the built-in status

- **WHEN** two agents declare different non-empty status values for the same lifecycle phase
- **THEN** that phase keeps its built-in status so the board's transitions remain well-defined

#### Scenario: Activating a card opens the shared preview

- **WHEN** a card is clicked or activated with the keyboard
- **THEN** the shared read-only object preview drawer opens for the card's object, with the item's status-gated actions available in the preview's action slot

#### Scenario: Machine cannot self-complete

- **WHEN** a card has not been completed or approved
- **THEN** it is not shown in the done column

#### Scenario: Unfiled sessions excluded

- **WHEN** a session has no associated work object
- **THEN** it does not appear on the board
