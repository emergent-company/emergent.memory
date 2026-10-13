## ADDED Requirements

### Requirement: Work statuses are presented as humanised labels

Wherever a work status is shown to a user — the object preview (badge and
details), the objects list and detail badges, the board lanes, and the schema
browser — it SHALL be presented as a humanised label, not as the raw status key,
so the same status reads identically across surfaces.

#### Scenario: A raw status key renders as a label

- **WHEN** an object whose status is `in_progress` is shown in the preview or the objects list
- **THEN** the status is presented as its humanised label (`In progress`), not the raw key

#### Scenario: Status presentation is consistent across surfaces

- **WHEN** the same status is shown on a board lane, in the schema browser, and in the object preview
- **THEN** all three present the same humanised label

#### Scenario: The raw value is still available

- **WHEN** a status is rendered
- **THEN** the raw value remains available on the element (for machine consumers and tests) even though the visible text is humanised

#### Scenario: Empty status keeps its empty state

- **WHEN** an object has no status
- **THEN** the status is rendered in the existing de-emphasised empty state rather than as an empty label
