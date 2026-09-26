## Purpose

Defines the accessibility floor for gateway UI surfaces: keyboard reachability of interactive rows,
minimum touch-target sizes for row actions, per-field validation feedback, and the rule that status is
never conveyed by colour alone.

## ADDED Requirements

### Requirement: Interactive rows are keyboard reachable

A list row, card, or table row that acts as a navigation target SHALL be reachable and activatable from
the keyboard, and SHALL NOT rely solely on a mouse handler attached to a non-focusable element.

#### Scenario: Row target is focusable

- **WHEN** a row carries a navigation target rendered on a container element
- **THEN** that element is focusable and activatable by keyboard, or the row contains a real link covering the same destination

#### Scenario: Focus is visible

- **WHEN** a row target receives keyboard focus
- **THEN** it carries the focus-visible treatment, asserted at the source (the focus style is present on the target), with the visual result verified by the browser audit

### Requirement: Row actions meet the minimum touch-target size

Icon-only row actions SHALL present a hit area of at least 44 by 44 CSS pixels on coarse-pointer devices.

#### Scenario: Row actions meet the minimum touch-target size

- **WHEN** a row renders icon-only actions and the device reports a coarse pointer
- **THEN** the shared row-action style emits a coarse-pointer rule giving each action a hit area of at least 44 by 44 CSS pixels, or the actions are moved into an overflow control that meets that size

#### Scenario: The coarse-pointer rule is asserted at the source

- **WHEN** the compiled stylesheet is inspected
- **THEN** it contains the coarse-pointer rule with the minimum hit-area declaration, since hit geometry cannot be asserted from rendered HTML in a unit test; the behavioural check is performed by the browser audit

#### Scenario: Desktop density is preserved

- **WHEN** the same row renders on a fine-pointer device
- **THEN** the desktop density is unchanged

### Requirement: Icon-only controls carry an accessible name

An icon-only control SHALL expose an accessible name, and an icon that conveys meaning SHALL NOT be
hidden from assistive technology without an equivalent text alternative.

#### Scenario: Icon button is named

- **WHEN** a control renders only an icon
- **THEN** it exposes an accessible name

### Requirement: Form validation failures identify the field

A form submission rejected for invalid input SHALL indicate which field failed, not only that the
submission failed.

#### Scenario: Field-level error is shown

- **WHEN** a submission is rejected because a specific field is invalid
- **THEN** that field renders an associated error message and is marked invalid for assistive technology

#### Scenario: Error is programmatically associated

- **WHEN** a field renders a validation error
- **THEN** the control is associated with its error message so assistive technology announces it

### Requirement: Status is not conveyed by colour alone

Status and state SHALL carry a non-colour signal, such as text or a shape difference, in addition to any
colour treatment.

#### Scenario: Status has a text signal

- **WHEN** a status indicator communicates state through colour or animation
- **THEN** it also exposes a text or shape signal that does not depend on colour perception
