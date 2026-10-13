## ADDED Requirements

### Requirement: The object preview shows the object's change history

The shared object preview drawer SHALL render the object's version/change
history — the object's versions in newest-first order, each with its version
marker, status, and creation time — for both chat-object and board-card
previews.

#### Scenario: History renders newest first

- **WHEN** the preview is opened for an object with more than one version
- **THEN** the history lists the versions newest-first and each entry shows the version marker, the status, and the creation time

#### Scenario: History is shared across preview surfaces

- **WHEN** the preview is opened for a board card and for a chat object reference
- **THEN** both show the history section

#### Scenario: An object with no extra versions

- **WHEN** the preview is opened for an object with a single version
- **THEN** the history section renders that version rather than an error

### Requirement: History loads without delaying the preview

The history section SHALL load independently of the initial preview render, and
a failure to load the history SHALL degrade to a visible failure state inside
the section rather than breaking the rest of the preview.

#### Scenario: The preview renders before history arrives

- **WHEN** the drawer opens
- **THEN** the object summary renders immediately and the history section fills in when its own request completes

#### Scenario: History request fails

- **WHEN** the history request fails
- **THEN** the history section shows a failure state and the rest of the preview remains usable

#### Scenario: The drawer does not scroll sideways

- **WHEN** the history section renders
- **THEN** it fits the drawer width without introducing horizontal scrolling
