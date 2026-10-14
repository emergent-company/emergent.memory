## ADDED Requirements

### Requirement: A work item is settable to an exact status

The platform SHALL expose a work-path endpoint that sets a work item to an exact
status, reusing the same validation and transition machinery as the other human
actions. Setting the item's review status SHALL mark it `needs_review`; setting
any other status SHALL clear `needs_review`. The review attribution
(`reviewed_by`/`reviewed_at`) SHALL be left untouched. The transition SHALL emit
`status_changed` (waking matching listeners) but SHALL NOT enqueue a run. The
existing agent-write guard SHALL still apply: an agent-originated exact-status
write SHALL be rejected.

#### Scenario: Exact status set

- **WHEN** a human sets a work item to a valid status
- **THEN** the item transitions to that status and the response returns the updated item

#### Scenario: Empty status rejected

- **WHEN** the endpoint is called with an empty status
- **THEN** the request is rejected with a 400 and the item is unchanged

#### Scenario: Disallowed status rejected

- **WHEN** the endpoint is called with a status outside the item type's allowed set
- **THEN** the request is rejected and the item is unchanged

#### Scenario: Review target marks needs_review

- **WHEN** a human sets a work item to the review status
- **THEN** the item is marked `needs_review` and its review attribution (`reviewed_by`/`reviewed_at`) is unchanged

#### Scenario: Non-review target clears needs_review

- **WHEN** a human sets a work item to a status other than review
- **THEN** the item's `needs_review` is cleared and its review attribution is unchanged

#### Scenario: No run is enqueued

- **WHEN** a human sets a work item to an exact status
- **THEN** no agent run is enqueued by that action

#### Scenario: Agent-originated write rejected

- **WHEN** an agent (not a project member) calls the exact-status endpoint
- **THEN** the request is rejected and the item is unchanged

### Requirement: Board assignee and status are edited through constrained dropdowns

The board SHALL offer the item's assignee through a ticket-style dropdown whose
options are `Unassigned`, the project's runtime agents (by exact name), and the
project's members (by display name, falling back to email). The board SHALL offer
the item's status through a "Set status" dropdown whose options are the work-path
vocabulary minus the item's current status. Selecting an option SHALL post the
exact value to the corresponding work-path route.

#### Scenario: Assignee picker lists real assignees

- **WHEN** the board drawer renders a work item that is not done
- **THEN** it renders an assignee dropdown listing Unassigned, the runtime agents, and the members

#### Scenario: Assignee picker posts the exact value

- **WHEN** an operator selects an assignee option
- **THEN** the item is reassigned to the selected value through the reassign route

#### Scenario: Status control omits the current status

- **WHEN** the board drawer renders a work item
- **THEN** the "Set status" dropdown lists the work-path vocabulary minus the item's current status

#### Scenario: Status control posts to the set-status route

- **WHEN** an operator selects a status option
- **THEN** the item is set to that status through the set-status route

#### Scenario: Done item hides the assignee picker

- **WHEN** a work item is done
- **THEN** the assignee picker is not rendered
