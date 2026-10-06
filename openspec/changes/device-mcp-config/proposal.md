## Why

Devices connect to a project as MCP relay nodes and can host local MCP servers, but the
configuration of those servers lives **only on the device** — a local YAML file mutated by a
loopback-only management API. The Memory assistant/agent cannot see or configure them, so every
device must be set up by hand. The earlier Diane implementation kept the configuration in Memory
(`MCPProxyConfig` / `MCPSecret` graph objects, matched by scope and pushed to the node, with the
node watching for changes) and that capability was lost in the connector rewrite.

This change makes device MCP configuration **server-authoritative**: it is created, stored, and
edited in Memory, and pushed to the connected device over the existing MCP relay.

## What Changes

- **Device-targeted registry rows.** `kb.mcp_servers` gains a `target` (`server` | `device`) and a
  nullable `target_instance_id` (null = all devices in the project). A `device` row is **not**
  connected outbound by the server; it is delivered to the matching relay node(s).
- **Relay control frame.** The relay protocol gains a server→device `config` frame carrying the
  resolved server list (name/transport/enabled/command/args/env/url/headers/disabled_tools). The
  server pushes on registry change and on device connect (reconnect hydration); the device replies
  with an ack that reports only status/tool-count, never secret values.
- **Connector applies live.** The connector handles the frame by validating, persisting, and
  applying through its existing hot-reload path, then re-registers so the hub sees the new tool
  list immediately. Pushed secrets are stored locally via the connector secret store.
- **Assistant surface.** The existing MCP-server agent tools accept `target` / `target_instance_id`,
  and a new tool lists a device's server configuration, so the assistant can configure MCPs on
  connected devices.
- **Secret transport (revised invariant).** For server-managed device servers, credentials travel
  from Memory to the device over the authenticated TLS relay and are stored AES-GCM encrypted at
  rest server-side. The device never echoes secret values back. Locally-configured servers keep the
  original "stays on the device" behaviour.

## Capabilities

### New Capabilities

- `device-mcp-config`: server-authoritative device MCP configuration — the registry targeting,
  the relay `config` control frame, the connector apply/ack contract, and the secret-transport
  rules.

### Modified Capabilities

- `mcp-connector`: the connector applies server-pushed configuration and stores server-managed
  secrets locally.

## Impact

- **Server** (`apps/server/`):
  - `migrations/`: add `target` + `target_instance_id` to `kb.mcp_servers`.
  - `domain/mcpregistry/`: entity/repository/service target handling, exclusion of device rows from
    every outbound path (`FindAllEnabledTools`, slug resolver, discovery/sync, inspect, call),
    owner/admin authorization, agent-tool target params, and the `DeviceConfigPusher` interface.
  - `domain/mcprelay/`: `register` opt-in flag + owner capture, `FrameConfig` + `ConfigFrame`
    (with version), `Session.SendConfig`, non-blocking push-to-instance / push-to-project, and
    post-ack hydration. **No new HTTP push route** (push is in-process only).
  - `domain/mcp/`: register the new/updated agent tool names and the opt-in gate.
- **Connector** (`apps/connector.linux/`, whose Go engine runs under the mac app):
  - `internal/relay/`: `ConfigFrame` parse + off-loop, panic-recovered apply + ack + version guard.
  - `internal/config/` + `cmd/memory-connector/`: opt-in flag, apply via `registryManager.reload`,
    persist only after a successful reload, store secrets, never log/echo them.
- **Mac app** (`apps/connector.mac/`): the bundled Go engine inherits push handling; phase 2 adds
  the Swift UI surface (Memory-managed vs locally-configured servers).
- **Specs**: new `device-mcp-config` spec; delta to `mcp-connector`.
