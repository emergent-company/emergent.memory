# Design — add-relay-node-registry

## Context

Recon (gateway): no local DB; the persistence primitive is the per-project
settings KV (`gateway/settings.go` `ProjectSetting` + `Get/Set/DeleteProjectSetting`,
category/key, value is a JSON **object**). The device-key registry
(`gateway/setup.go:158-257`) is the copy-me template for a read-modify-write
list with add/remove. The MCP nodes page (`gateway/mcp_nodes.go`,
`mcp_nodes.templ`) renders live sessions from `ListRelaySessions` +
`GetRelaySessionTools`; rows are `<a>` links and there is no last-seen anywhere.
Memory relay sessions are in-memory: an absent node simply disappears.

## Goals / Non-Goals

Goals: keep a per-project node registry; show offline nodes with an offline
badge + last-seen; cache the last tool snapshot for offline nodes; allow manual
removal. Non-goals: backend persistence, TTL/auto-expiry, background poller,
connector/app changes.

## Decisions

### 1. Storage: project settings KV, object value

Category `mcp_relay_nodes`, key `registry`, value:
```json
{"nodes": {"<instanceID>": {
  "version": "...", "toolCount": 3,
  "tools": [{"name": "...", "description": "..."}],
  "firstSeen": "RFC3339", "lastSeen": "RFC3339"
}}}
```
Helpers mirror `deviceRegistry`/`issueDeviceKey`/`revokeDeviceKey`:
`loadRelayRegistry`, `upsertRelayNode`, `removeRelayNode`. 404 → empty registry.
Read-modify-write is unlocked (accepted single-admin risk, same as devices).

### 2. Lazy reconciliation on page load (no poller)

`uiMCPNodes` flow:
1. Load registry (settings KV).
2. Fetch live sessions (`ListRelaySessions`).
3. Upsert every live node (`lastSeen = now`, refresh version/toolCount; cache
   tools lazily — the tool list is already fetched for display, so store the
   snapshot on upsert when available, or on tools-panel load for the selected
   node).
4. Build a unified row list: **live** nodes (from sessions, sorted by
   `connected_at` desc) then **offline** nodes (registry records not live,
   sorted by `lastSeen` desc). Offline uses the cached tool snapshot.
5. If the live fetch fails, still render the registry (all offline) plus a
   banner — better than a blank page.

No background ticker: presence accuracy is bounded by page visits, which is
acceptable here (and matches the "no polling" idiom).

### 3. Tools for the selected node

Live → `GetRelaySessionTools`. Offline → the stored `tools` snapshot; empty
snapshot → "no tools recorded" state. Nothing new is stored on the backend.

### 4. Removal: form POST + PRG

Route `POST /settings/mcp-nodes/remove` (form field `instance_id`) →
`removeRelayNode` → `303 /settings/mcp-nodes?updated=1`; errors via
`redirectWithError`. The row currently is a whole `<a>`; the Remove control is a
**sibling form/button** (link row + right-aligned destructive button) so no
interactive element is nested. A removed **live** node reappears on the next
load (re-observed) — expected and documented in the UI copy.

### 5. Rendering

Rows get a state badge (live = warning/soft as today; offline = neutral/muted)
and, for offline nodes, `last seen <relative>`; live keeps `connected <rel>`.
Selection (`?node=`) works for both; the tools panel shows snapshot vs live.
Removal is offered for all stored nodes, with the copy implying offline cleanup.

## Risks / Trade-offs

- [Unlocked read-modify-write] → single-admin UI; accepted (documented like devices).
- [Last-seen granularity = page visits] → acceptable; can add a supervisor ticker later if sub-visit accuracy is needed.
- [Removing a live node reappears] → expected; UI copy says so.
- [Tool snapshot staleness for offline nodes] → cached tools labelled with the node's last-seen.
- [Registry growth] → manual removal; TTL is a follow-up.

## Migration Plan

Additive; no migration. First page load seeds the registry from live sessions.
Rollback = ignore/delete the settings entry.
