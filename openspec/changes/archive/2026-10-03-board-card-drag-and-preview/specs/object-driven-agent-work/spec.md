## MODIFIED Requirements

### Requirement: Kanban projection

The board SHALL be a read projection over board-enabled object types joined to their runs, with no separate work-item store. A board card SHALL be draggable only for the board's supported transitions — `blocked → ready` (execute), `review → done` (approve), `review → revision` (open the request-changes form), and any non-`done → blocked` (cancel); any other pair is rejected in the UI. The board does not derive draggable lanes from a type's declared work statuses, so a card whose status has no mapped action is not draggable. Activating a card SHALL open the shared read-only object preview rather than a board-specific summary.

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

#### Scenario: Activating a card opens the shared preview

- **WHEN** a card is clicked or activated with the keyboard
- **THEN** the shared read-only object preview drawer opens for the card's object, with the item's status-gated actions available in the preview's action slot

#### Scenario: Machine cannot self-complete

- **WHEN** a card has not been completed or approved
- **THEN** it is not shown in the done column

#### Scenario: Unfiled sessions excluded

- **WHEN** a session has no associated work object
- **THEN** it does not appear on the board
