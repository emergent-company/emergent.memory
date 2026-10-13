# Change: MCP registry device install

## Why

The registry browse page can only install remote (http/sse) entries. Every
package-only entry — the majority of the curated catalog — renders a dead end:
a note telling the user to install it locally through the connector, with no
action. The connector can already run these packages, and memory already models
device-targeted servers (`target = 'device'` pushed over the relay), but nothing
connects the two from the UI.

## What changes

- **Server** (`domain/mcpregistry`): `InstallFromRegistry` gains a device path.
  A `target = "device"` install translates the entry's first locally-runnable
  package (npm → `npx -y`, pypi → `uvx`, registry order) into a stdio server
  carrying the package's rendered arguments, declared env placeholders and
  required configuration, targets it at the requested relay instance, and lets
  the existing `CreateServer` authorization and relay push take over. The
  outbound path (and its remote-only policy) is unchanged.
- **Gateway**: the registry page loads the connected devices and, for each
  package-only entry, renders an "Install to device" action that opens a dialog
  picking a connector; with no device connected the dialog shows the connector
  setup steps. Successful installs land on the connected devices page (which
  gains an install-confirmation flash); installs needing credentials land on the
  server's configure form, exactly like the outbound install.

## Impact

- Affected specs: `mcp-registry` (device install translation, modified install
  requirement), `mcp-servers-ui` (registry listing device install + feedback),
  `external-mcp-nodes` (install-confirmation flash).
- Affected code: `apps/server/domain/mcpregistry`, `apps/web-ui/gateway`.
- No migration: `target` / `target_instance_id` columns and the relay push path
  already exist.
- Out of scope: OAuth connect (`mcp-oauth-connect`), authoring device-targeted
  servers from the MCP servers form.
