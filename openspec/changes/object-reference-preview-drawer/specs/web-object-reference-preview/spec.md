## Purpose

A compact, read-only preview of a knowledge-graph object opened from a chat object reference, so a user can see what a reference points at without leaving the conversation or landing in the edit form.

## ADDED Requirements

### Requirement: Inline chat object references open a read-only preview drawer

Activating a validated object reference inside an assistant chat message, or a chat sources-list entry, SHALL open a read-only preview of the referenced object in a right-hand side drawer instead of navigating to the object edit view.

#### Scenario: Plain left-click opens the preview

- **WHEN** the user performs a plain primary-button click on an inline chat object reference
- **THEN** the object preview drawer opens from the right
- **AND** the browser does not navigate to the object edit view

#### Scenario: Modified clicks keep native navigation

- **WHEN** the user activates the reference with a modifier key (cmd/ctrl/shift/alt), a non-primary mouse button, or an anchor carrying `target`/`download`
- **THEN** the browser performs its native navigation to the object edit view
- **AND** the preview drawer does not open

#### Scenario: Interception is scoped to chat content

- **WHEN** an `/objects/<id>` link is rendered outside chat content (for example on the object list or detail page)
- **THEN** activating it navigates normally and the preview drawer is not opened

#### Scenario: Unresolvable references keep their existing behaviour

- **WHEN** a reference href is not a single-segment canonical object id (for example a multi-segment human key)
- **THEN** the drawer interception does not apply and the link behaves as before

### Requirement: Preview presents a read-only label/value summary

The drawer SHALL present the object's icon and type, its display label, and its properties as read-only label/value pairs, together with key metadata. The drawer MUST NOT render editable form controls for the object's fields and MUST NOT mutate the object.

#### Scenario: Properties render as labels and values

- **WHEN** the preview loads an object with schema-defined properties
- **THEN** each property is rendered as a label with its value
- **AND** no input, textarea, or select control is rendered for the object's fields

#### Scenario: Empty values are de-emphasised

- **WHEN** a property has no value
- **THEN** its row is rendered in a muted empty style and is marked so a future hide/fold toggle can target it

#### Scenario: Relationship context is surfaced when resolvable

- **WHEN** a reference carries a relationship fragment
- **THEN** the object is previewed and the relationship context is shown when it can be resolved
- **AND** a failure to resolve the relationship does not degrade or block the preview

#### Scenario: Object cannot be loaded

- **WHEN** the referenced object cannot be fetched
- **THEN** the drawer shows a non-degrading not-found state rather than an error or an empty shell

### Requirement: Drawer provides a jump-to-edit action

The preview drawer SHALL provide a control at the top that navigates to the existing object edit view for the previewed object.

#### Scenario: Edit action navigates to the edit view

- **WHEN** the preview drawer is open for an object and the user activates the edit control
- **THEN** the app navigates to `/objects/<id>` for that object

### Requirement: Drawer is accessible and dismissible

The drawer SHALL be an accessible modal dialog — exposing a dialog role, an accessible name, and keyboard focus handling — and SHALL be dismissible via a close control, a backdrop click, and the Escape key.

#### Scenario: Dismissal

- **WHEN** the drawer is open and the user presses Escape, clicks the backdrop, or activates the close control
- **THEN** the drawer closes and focus is not trapped

#### Scenario: Survives in-app navigation

- **WHEN** the user navigates between in-app views that swap the main content region
- **THEN** the drawer remains mounted and its open/closed state is not destroyed by the swap

### Requirement: Preview content is loaded from a gateway partial route

The drawer body SHALL be fetched from a gateway partial route (`GET /objects/<id>/preview`) that reuses the existing object, relationship, and compiled-type fetch seam, and SHALL show a loading state while the content is in flight.

#### Scenario: Partial route serves the drawer body

- **WHEN** the drawer opens for an object id
- **THEN** it requests `GET /objects/<id>/preview` and renders the returned partial into the drawer body
