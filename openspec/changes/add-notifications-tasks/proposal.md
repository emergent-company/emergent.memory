> **Superseded (notifications portion).** The notifications capability defined here is superseded by `openspec/changes/add-inbox-subsystem/`, which adds account/project scopes, the event taxonomy, opt-in preferences, actionable items, and the producer API on top of the same `kb.notifications` store and UI surface. This change is retained only for the **background-task monitor** (`tasks` capability). Once `add-inbox-subsystem` lands, close or archive this change's notifications portion.

## Why

The previous Memory UI had a notifications inbox (with real-time SSE) and a background-task monitor. Alfred has neither.

## What Changes

- Add notification listing, counts, mark-read, and dismiss, with real-time updates (SSE).
- Add task listing, counts, resolve, and cancel.

## Capabilities

### New Capabilities
- `notifications`: list, read, and dismiss notifications with real-time updates.
- `tasks`: list, resolve, and cancel background tasks.

### Modified Capabilities
<!-- none -->

## Impact

- `gateway/`: Memory client methods for notifications + tasks, SSE stream, handlers, routes.
- `gateway/ui.go` + templ: notifications inbox and task monitor.
