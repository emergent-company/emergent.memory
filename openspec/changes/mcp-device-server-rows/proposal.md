## Why

`FindAllServers` returns MCP server rows for every target, including
`target = 'device'` rows that are pushed to a connector over the relay. The
gateway's `MCPServer` wire type omitted `target` / `targetInstanceId`, so a
device-targeted row rendered in the MCP Servers list as an ordinary outbound
server: no indication it runs on a connector rather than the shared host, no
indication which device the config is bound to, and outbound-only affordances
(connect / tool sync) were shown anyway even though the host never dials the
server.

## What Changes

- **Wire type**: `MCPServer` (`extras.go`) gains `Target` and
  `TargetInstanceId`, mirroring the server's `MCPServerDetailDTO` fields.
- **Row rendering** (`mcp_servers.templ`): a device-targeted row shows a
  `device` badge plus its target instance id when set, and an expanded-body
  note explaining the server runs on a connector. The outbound-only affordances
  (sync, inspect, OAuth connect) are dropped from device rows; the "no tools
  cached" hint stops pointing at a host sync and instead notes tools are
  discovered on the device.
- **Out of scope**: the create/edit form still cannot author a device-targeted
  server (only a registry device install creates one); that remains unchanged.

This changes user-visible behaviour in the `mcp-servers-ui` capability.
