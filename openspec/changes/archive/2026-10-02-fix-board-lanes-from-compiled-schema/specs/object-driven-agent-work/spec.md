## Purpose

Defines how the Kanban board derives its column (lane) order. Lanes are a
projection of the schema, not a hard-coded constant: the compiled board-enabled
object types declare their allowed work statuses, and the board renders those in
declaration order, falling back to the canonical order only when the schema
declares none. Shadowed (overridden) types never contribute lanes.

## ADDED Requirements

### Requirement: Board lanes derive from schema-declared work statuses

The board SHALL derive its lane order from the compiled schema's board-enabled
object types' `allowedStatuses`, in declaration order and de-duplicated
first-seen. It SHALL fall back to the canonical lane order only when no
board-enabled type declares any statuses (or the compiled schema cannot be
fetched/parsed). Statuses present on work items but absent from the declared
schema SHALL be preserved as extra lanes, sorted after the declared ones, so no
item is hidden. Shadowed (losing duplicate) object types SHALL NOT contribute
lanes, consistent with every other compiled-type consumer.

#### Scenario: Custom schema statuses replace canonical lanes

- **WHEN** the compiled board-enabled type declares `allowedStatuses: [todo,
  doing]`
- **THEN** the board renders `todo` and `doing` lanes and does not render the
  canonical `done` lane

#### Scenario: Canonical lanes when the schema declares none

- **WHEN** the compiled schema has no board-enabled type declaring statuses (or
  the fetch fails)
- **THEN** the board renders the canonical lane order

#### Scenario: Unknown item status is preserved

- **WHEN** a work item carries a status not present in the declared schema
- **THEN** the board still renders a lane for it, after the declared lanes

#### Scenario: Shadowed type does not leak lanes

- **WHEN** two object types share a name and the earlier (shadowed) one declares
  a status the effective winner does not
- **THEN** the shadowed status is not rendered as a lane
