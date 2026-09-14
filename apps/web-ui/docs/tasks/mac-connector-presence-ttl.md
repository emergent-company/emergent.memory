# Mac connector presence TTL

**Status:** proposed
**Created:** 2026-09-13
**Source:** [2026-09-13-memory-mac-connector](../sessions/2026-09-13-memory-mac-connector.md)

## What
Add accurate presence/last-seen for relay nodes: a gateway background reconciler
(~30s tick) that polls live relay sessions and updates the node registry, marking
nodes offline after a TTL (e.g. 2 minutes without observation), instead of relying
on page-load reconciliation only.

## Why
The registry currently updates `lastSeen` lazily when the MCP nodes page is
loaded, so offline time is visit-bounded. Legacy Diane used a heartbeat TTL
sweeper (30s tick / 2min timeout) for precise `Disconnected` + `last seen`.

## Depends on
- `gateway/mcp_nodes_registry.go` (registry helpers exist).
- Gateway supervisor ticker pattern (`gateway/supervisor.go`).
- Project scoping for a background poll (needs a service path or poll only
  projects with registry entries).

## Notes
- Revisit whether polling needs project enumeration; page-load reconciliation
  can stay as a fallback.
- Do not repeat Diane's mistake of a UI action hitting a stub endpoint.
