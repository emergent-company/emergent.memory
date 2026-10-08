## Why

The `mcp-required-config` change gated registry installs on required configuration
and named the missing keys, but the UI only ever shows the key **names**. Registry
entries declare far more: GitHub's remote declares an `Authorization` header with
`description: "Authorization header with authentication token (PAT or App token)"`,
and some entries (e.g. Netdata) declare a value template such as
`Bearer {NETDATA_CLOUD_API_TOKEN}`. `collectRegistryRequiredConfig` reads only
`Name`/`IsSecret`/`IsRequired`, so that guidance is dropped before it reaches the API
or the UI. A user installing GitHub's MCP server sees "Missing: Authorization" with
no indication of what kind of value to supply.

## What Changes

- Carry the registry's `description` (header and env) and header `value` template
  through `RequiredConfigKey`, persistence (`kb.mcp_servers.required_config`), and the
  API DTOs.
- Render a required row's description as a hint under the row on the create/edit form.
- Use a required key's value template as the value input's placeholder (header
  transports only) so the expected shape is visible. It is a placeholder only —
  never prefilled, so no credential leaks.

## Capabilities

### Modified Capabilities

- `mcp-registry`: required configuration keys retain the registry's description and
  header value template.
- `mcp-servers-ui`: required rows surface the registry's guidance on the form.

## Impact

- Server (`apps/server/domain/mcpregistry`): `RequiredConfigKey`, `collectRegistryRequiredConfig`,
  a migration refreshing the column comment (+ unit tests).
- Web UI (`apps/web-ui/gateway`): `extras.go`, `mcp_servers.go`, `mcp_servers.templ`
  (+ unit tests).
- Additive JSONB shape; existing `required_config` rows without the new fields keep
  working (both fields are `omitempty`).
