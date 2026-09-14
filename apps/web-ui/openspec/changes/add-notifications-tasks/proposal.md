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
