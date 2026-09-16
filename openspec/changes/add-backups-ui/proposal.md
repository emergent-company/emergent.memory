## Why

The previous Memory UI supported project backups (create, restore, download, checksums). Alfred has no backup surface.

## What Changes

- Add backup creation, listing, download, restore, and deletion.
- Show backup status fields and checksums.

## Capabilities

### New Capabilities
- `backups`: create, list, restore, download, and delete project backups.

### Modified Capabilities
<!-- none -->

## Impact

- `gateway/`: Memory client methods for backups, handlers, routes.
- `gateway/ui.go` + templ: backups screen.
