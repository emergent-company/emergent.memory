## ADDED Requirements

### Requirement: Object status is edited through a constrained control

The object create and edit forms SHALL render the built-in `status` field as a
native select whose options are the object type's declared `allowedStatuses`
when present, otherwise the canonical board work statuses. The select SHALL
apply to every type that reaches the form — including a type with no board
config, whose objects are still projected onto the board through the
canonical-lane fallback — so a status field whose type resolves to an option set
is never a free-form text input. When the compiled types cannot be loaded, the
edit form SHALL fall back to a free-form text input. The
select SHALL mark the object's current status as selected, SHALL keep a stored
status that is not in the option set as an extra selected option so saving never
drops it, and SHALL offer a blank pick option when the current status is empty.

#### Scenario: Declared statuses render a select

- **WHEN** a type declares `allowedStatuses: [ready, in_progress, done]`
- **THEN** the create and edit forms render a select with one option per declared status
- **AND** the object's current status is the selected option

#### Scenario: Canonical fallback when no statuses are declared

- **WHEN** a type declares no allowed statuses
- **THEN** the select offers the canonical board order

#### Scenario: Type with no board config still gets a select

- **WHEN** a type's registry row declares neither `boardEnabled` nor `allowedStatuses` (its objects still render on the board via the canonical-lane fallback)
- **THEN** the create and edit forms render a select of the canonical board order
- **AND** do not fall back to a free-form text input

#### Scenario: Stored status outside the option set is preserved

- **WHEN** an object carries a status not present in its option set
- **THEN** that value is rendered as an extra selected option so saving does not drop it

#### Scenario: Blank pick option when empty

- **WHEN** an object has no status
- **THEN** the select renders a blank selected pick option

#### Scenario: Text fallback when compiled types cannot be loaded

- **WHEN** the compiled types cannot be loaded while rendering the object edit form
- **THEN** the status field falls back to a free-form text input
