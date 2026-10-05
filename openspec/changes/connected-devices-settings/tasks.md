# Tasks — connected-devices-settings

TDD in the gateway module (Go tests with the existing fakes). Verify with
`templ generate ./...`, `go generate ./...` (component graph), `go build ./...`,
`go test ./...`, and `task lint` from `apps/web-ui/gateway`.

## 1. Spec

- [x] 1.1 Proposal documenting the merge, the preserved-behavior intent, and the
  distinct credential/relay data models underneath.
- [x] 1.2 Delta spec `settings-navigation`: one **Connected devices** rail entry;
  "Open Connected devices" scenario; legacy `/settings/mcp-nodes` redirect
  scenario.
- [x] 1.3 Delta spec `external-mcp-nodes`: nodes render under **MCP nodes** in
  the Connected devices page at `/settings/devices`; legacy route 303-redirects
  preserving `?node=`.

## 2. Handler + data

- [x] 2.1 `connectedDevicesPageData` (settings_handlers.go) embedding
  `settingsNavCommon` + iOS QR, devices, MCP node fields, and flash.
- [x] 2.2 `uiConnectedDevices` composes the device data with the relay
  reconciliation.
- [x] 2.3 Factor reconciliation + `?node=` selection out of `uiMCPNodes` into
  `loadMCPNodesData(ctx, selectedNode)`, preserving LoadErr/ToolsErr/NodeNotFound
  degradation semantics exactly.
- [x] 2.4 `uiMCPNodesRedirect` → 303 to `/settings/devices`, forwarding the query
  verbatim (`?node=` preserved).

## 3. Routes + nav

- [x] 3.1 `main.go`: `GET /settings/devices` → `uiConnectedDevices`;
  `GET /settings/mcp-nodes` → `uiMCPNodesRedirect`; remove/revoke POST routes
  unchanged.
- [x] 3.2 `settingsSubNav`: single `devices` entry, Href `/settings/devices`,
  Icon `lucide--devices`, Label `Connected devices`; `mcp-nodes` entry removed.

## 4. Templates

- [x] 4.1 `ConnectedDevicesPage` renders Connect a device, Devices, and MCP
  nodes sections in order.
- [x] 4.2 Retire `DevicesSettingsPage` and `ExternalMCPNodesPage`; keep the
  shared panels/rows/dialogs; `templ generate` (never hand-edit `*_templ.go`).
- [x] 4.3 `iosSetupPanel` retitled "Connect a device" with the Mac/Linux
  `memory-connector auth login` OAuth note.

## 5. Tests

- [x] 5.1 `project_settings_ui_test.go`: sub-nav expects the single Connected
  devices entry; `TestRenderDevicesSettingsPage` → `TestRenderConnectedDevicesPage`
  covering header + all three sections + empty states.
- [x] 5.2 `mcp_nodes_ui_test.go`: sub-nav entry test reworked; MCP render tests
  target `ConnectedDevicesPage`; `TestUIConnectedDevicesRoute`; new
  `TestUIMCPNodesRedirect` (303 + `/settings/devices`, `?node=`, `?updated=`).
- [x] 5.3 `settings_handlers_test.go` and `mcp_nodes_registry_test.go` updated
  for the merged page and redirect.
- [x] 5.4 Grep the gateway test tree for stale `ExternalMCPNodesPage` /
  `DevicesSettingsPage` / `/settings/mcp-nodes` references.

## 6. Verification

- [x] 6.1 `templ generate ./...`
- [x] 6.2 `go generate ./...` (component graph fresh; `TestGraphStaleCheck` green)
- [x] 6.3 `go build ./...`
- [x] 6.4 `go test ./...` green
- [x] 6.5 `task lint` clean
