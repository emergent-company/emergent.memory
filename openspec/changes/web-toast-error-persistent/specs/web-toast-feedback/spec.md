## Purpose

Toast feedback severity semantics for the Memory gateway: error toasts persist
until the user dismisses them, every other kind auto-dismisses.

## ADDED Requirements

### Requirement: Error toasts are persistent

The gateway SHALL render `error` toasts without an auto-dismiss timer and SHALL
render an explicit, accessible close control on them. An error toast MUST be
dismissed only through that control.

#### Scenario: Error toast stays until dismissed

- **WHEN** an action fails and an `error` toast is shown (server flash or client-invoked)
- **THEN** the toast does not auto-dismiss
- **AND** it renders a close control with an accessible name
- **AND** activating the close control removes the toast

#### Scenario: Clicking an error toast body does not dismiss it

- **WHEN** the user clicks an error toast's body (not its close control)
- **THEN** the toast remains

### Requirement: Non-error toasts auto-dismiss

The gateway SHALL auto-dismiss `success`, `info`, and `warning` toasts after a
short timeout and SHALL allow dismissing them by clicking them.

#### Scenario: Non-error toast auto-dismisses

- **WHEN** a `success`, `info`, or `warning` toast is shown
- **THEN** it is removed after the shared auto-dismiss timeout
- **AND** clicking it removes it immediately

### Requirement: One severity-to-duration rule

Server PRG flash feedback and client-originated toasts SHALL derive the toast
duration from the same severity rule: `error` is persistent (duration 0) and
every other kind uses the shared auto-dismiss timeout.

#### Scenario: Server flash of an error is persistent

- **WHEN** a handler flashes an error through `flashToast("error", …)`
- **THEN** the emitted toast payload carries duration 0

#### Scenario: Client toasts share the rule

- **WHEN** client code calls `toast("error", …)`
- **THEN** it is added to the queue with duration 0
- **AND** `toast("success", …)` is added with the shared auto-dismiss timeout
