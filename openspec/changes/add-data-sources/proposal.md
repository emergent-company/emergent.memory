## Why

The previous Memory UI could connect external systems (email, Google Drive, ClickUp) and sync them into the graph. Alfred currently has no data-source support.

## What Changes

- Add data-source connection, configuration, and connection testing.
- Add sync triggering, monitoring, and cancellation.
- Add schema discovery for connected sources.

## Capabilities

### New Capabilities
- `data-sources`: connect, sync, and monitor external data sources.

### Modified Capabilities
<!-- none -->

## Impact

- `gateway/`: Memory client methods for data sources + sync jobs, handlers, routes.
- `gateway/ui.go` + templ: data-source list/connect/sync screens.
- OAuth/callback handling for provider auth (email/GDrive).
