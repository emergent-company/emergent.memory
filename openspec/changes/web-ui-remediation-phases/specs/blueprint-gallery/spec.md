## Purpose

Routes the blueprint pack-removal action through the shared destructive confirmation, resolving the
conflict between this capability's "remove on activation" scenario and the gateway-wide convention that
every destructive action is confirmed through `ui.ConfirmDialog`.

## MODIFIED Requirements

### Requirement: Remove a pack from the UI

The UI SHALL allow soft-removing an installed pack, and the removal SHALL be confirmed through the shared destructive confirmation dialog before any request is issued.

#### Scenario: Remove a pack

- **WHEN** a user activates the remove action on an installed pack
- **THEN** the shared confirmation dialog is shown, and the pack is removed from the installed list only after the user confirms, with its data remaining recoverable

#### Scenario: Removal is cancelled

- **WHEN** the user dismisses the confirmation dialog without confirming
- **THEN** no removal request is sent and the pack remains installed
