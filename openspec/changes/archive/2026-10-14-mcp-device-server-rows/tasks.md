# Tasks — mcp-device-server-rows

TDD throughout: each implementation task ships with the unit test named beside
it. Build/test only in `apps/web-ui/gateway`.

## 1. Wire type carries the target fields

- [x] 1.1 Add `Target` / `TargetInstanceId` to the gateway `MCPServer` struct
  in `extras.go` (`json:"target,omitempty"` / `json:"targetInstanceId,omitempty"`).
- [x] 1.2 Add `mcpServerIsDevice(s MCPServer) bool` in `mcp_servers.go` and use
  it to keep `mcpServerOAuthShown` false for device rows.

## 2. Row rendering distinguishes device from outbound

- [x] 2.1 Render a `device` badge (plus the target instance id when set) in the
  list summary for device-targeted rows.
- [x] 2.2 Drop sync / inspect actions from the expanded action toolbar for
  device rows; add an expanded-body note explaining the server runs on a
  connector.
- [x] 2.3 Relabel the "no tools cached" hint for device rows (tools are
  discovered on the device) and suppress the "sync refreshes this list" hint.

## 3. Tests

- [x] 3.1 Add `TestUIMCPServersListDeviceRow` in `mcp_servers_handlers_test.go`
  covering a bound device row and a broadcast device row alongside an outbound
  row, asserting the two render differently (device badge / instance id / note
  present, sync/inspect absent on device rows and present on the outbound row).

## 4. Verification

- [x] 4.1 `templ generate ./...` (regenerate `*_templ.go`; never hand-edited).
- [x] 4.2 `go build ./...`, `go test ./...`, and `task lint` in
  `apps/web-ui/gateway`.
