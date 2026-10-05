## Why

Settings has two separate pages that both exist to connect external machines to
a project: **Devices** (`/settings/devices`) enrolls phones through an iOS setup
QR, and **MCP nodes** (`/settings/mcp-nodes`) shows the relay registry of
connector machines (Mac/Linux) and their tools. They answer the same user
question — "what is connected to this project?" — but are split across two rail
entries, so a user who set up a Mac connector has to know to look at a different
page than the one where they enrolled their phone.

## What Changes

- Merge the two pages into one **Connected devices** page at `/settings/devices`,
  with three stacked sections: **Connect a device** (the existing iOS setup QR,
  plus a note that Mac/Linux connectors enroll via `memory-connector auth login`
  OAuth), **Devices** (the registered device credentials), and **MCP nodes**
  (the reconciled relay registry and its tools panel).
- `GET /settings/mcp-nodes` becomes a `303` redirect to `/settings/devices`,
  forwarding the query verbatim so `?node=<id>` deep-links still land on the
  right row and PRG flash params still surface.
- The settings sub-navigation collapses the `Devices` and `MCP nodes` entries
  into a single `Connected devices` entry.
- Behavior is preserved: device credentials are still listed and revoked through
  `POST /settings/devices/:id/revoke`, and node selection (`?node=`), the tools
  panel, removal, and all empty/error/degradation states render exactly as
  before.
- Underneath, the two datasets stay **distinct**: device credentials live in
  `core.api_tokens` with the `device:api` scope, while relay nodes are live
  sessions reconciled into a per-project registry. This change is a UI
  consolidation only — no credential model, token shape, or relay session data
  changes.

## Capabilities

### Modified Capabilities

- `settings-navigation`: the rail now lists a single **Connected devices**
  section instead of separate **Devices** and **MCP nodes** entries, and the
  legacy `/settings/mcp-nodes` route redirects to it.
- `external-mcp-nodes`: the node list renders in the Connected devices page at
  `/settings/devices` under an **MCP nodes** section; the legacy
  `/settings/mcp-nodes` route redirects there.

## Impact

- `gateway/project_settings.templ` — new `ConnectedDevicesPage` composing the
  existing `iosSetupPanel`, `devicesPanel`, and the MCP node list/tools panels;
  single `Connected devices` rail entry.
- `gateway/mcp_nodes.templ` — retire `ExternalMCPNodesPage`; the node
  list/tools panels now read the combined page data.
- `gateway/settings_handlers.go` — `connectedDevicesPageData` and
  `uiConnectedDevices`, replacing `devicesSettingsPageData` /
  `uiProjectDeviceSettings`.
- `gateway/mcp_nodes.go` — the registry reconciliation and `?node=` selection
  are factored into `loadMCPNodesData`; `uiMCPNodes` becomes
  `uiMCPNodesRedirect`.
- `gateway/main.go` — reroute `GET /settings/devices` to `uiConnectedDevices`
  and `GET /settings/mcp-nodes` to `uiMCPNodesRedirect`; the remove and revoke
  POST routes are unchanged.
- No server, connector, iOS, or data-model changes.
