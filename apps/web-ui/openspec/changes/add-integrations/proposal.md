## Why

The previous Memory UI supported GitHub App and general third-party integrations. Alfred has no integration surface.

## What Changes

- Add GitHub App connect (OAuth), status, and disconnect.
- Add general integration list/configure/test/sync/delete.

## Capabilities

### New Capabilities
- `integrations`: connect, configure, test, and sync third-party integrations (GitHub App + general).

### Modified Capabilities
<!-- none -->

## Impact

- `gateway/`: Memory client methods for integrations, handlers, routes.
- `gateway/ui.go` + templ: integrations screen.
- OAuth callback handling for the GitHub App.
