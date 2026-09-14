## Why

Relay sessions are in-memory on the Memory backend: once a host disconnects,
its node disappears from the Memory UI with no trace. Users who configured a
machine (e.g. the `tool` Mac) cannot tell "never connected" from "was connected
and went offline", and cannot clean up a stale entry.

The gateway already has the right primitive: a per-project settings KV
(used for the device-key registry) and the MCP nodes page. This change makes
the gateway keep a small **node registry** so offline nodes stay visible with
an offline badge + last-seen time, and can be **removed manually**.

## What Changes

- **Gateway-side node registry** (per project, stored in the existing
  `/api/projects/:id/settings/<category>/<key>` KV): each observed relay node
  is recorded with instance id, version, tool count, tool snapshot, first-seen
  and last-seen.
- **Lazy reconciliation**: when the MCP nodes page loads, the handler fetches
  live sessions, upserts each live node (refresh version/tool count/last-seen),
  and leaves absent nodes in the registry as **offline**. No backend or
  background-poller changes.
- **UI**: rows show live vs offline state (badge) and "last seen <relative>"
  for offline nodes; the tool list is served from the cached snapshot when a
  node is offline.
- **Manual removal**: a Remove action (form POST + redirect) deletes a node
  from the registry, for cleaning up stale entries. Live nodes can be removed
  too (they will reappear on the next page load, since the backend session
  still exists) — the action is intended for offline entries.
- Out of scope: backend persistence of relay sessions, automatic expiry/TTL of
  registry entries, connector changes, and the CLI/app behaviour.

## Capabilities

### New Capabilities
- `relay-node-registry`: gateway-side persistence of observed relay nodes per
  project, with offline/last-seen tracking and manual removal.

### Modified Capabilities
- `external-mcp-nodes`: the page lists persisted nodes (live and offline) with
  state/last-seen and a removal control, instead of only live sessions.

## Impact

- `gateway/` only: settings-KV registry helpers (+ tests), `uiMCPNodes` lazy
  reconciliation, a new POST remove route + redirect, and the templ page.
- No backend (emergent.memory), connector, or macOS app changes.
- Read-modify-write on the settings KV is unlocked (same accepted risk as the
  device registry; single-admin UI).
