## Why

Users want to add local MCP servers to the macOS connector without hand-typing
command, args, and environment variables. Memory already integrates the
official MCP registry (`https://registry.modelcontextprotocol.io`) server-side
— authenticated project-scoped `search`/`get`/`install` routes, agent tools,
and SDK/gateway proxies — but the connector and its management UI have no
registry at all, so every local server is typed by hand.

The first target is Chrome DevTools MCP
(`io.github.ChromeDevTools/chrome-devtools-mcp`), an npm **stdio** package. The
server refuses stdio installs (it must not spawn arbitrary processes on the
shared host) and a DevTools MCP only makes sense running locally on the user's
own Mac, so this belongs on the connector.

## What Changes

- **Connector proxies the registry through Memory** (single registry client, no
  second upstream integration): the connector's loopback management API gains
  `GET /api/mcp-registry/search` and `GET /api/mcp-registry/servers/{name}`,
  backed by the existing project-scoped `/api/admin/mcp-registry/*` routes via
  the SDK and the connector's configured project token.
- **Local suggestion translation** on the connector: a registry entry becomes a
  suggested local `mcphost.ServerConfig` — **stdio-first** (`npm` → `npx -y
  <identifier>@<version>`, `pypi` → `uvx <identifier>`), with remote (`http`/`sse`)
  fallback — appending runtime/package arguments, surfacing env/header metadata
  (required/secret), and warning (not failing) when the required runner is not
  installed.
- **Mac app browse + prefilled review**: the hosted MCP servers page gains a
  "Browse registry" flow; selecting an entry opens the existing Add sheet
  prefilled with the suggested command/args/env (or url/headers). The user
  reviews, fills secrets, and confirms — nothing is created automatically.
- **Web UI registry page**: `/settings/mcp-registry` browses and installs from
  the existing admin routes. Remote (`http`/`sse`) entries are installable into
  the project; **stdio-only** entries (e.g. DevTools MCP) show guidance to
  install via the macOS connector instead of an Install button.
- **SDK**: registry `search`/`get` proxy methods plus DTOs on the existing MCP
  registry client, so the connector reuses it.
- **Out of scope**: server-side stdio install (stays blocked), registry caching,
  installed-server auto-update, and runners beyond `npx`/`uvx`.

## Capabilities

### New Capabilities

- `mcp-registry`: browse the official MCP registry through Memory, and install
  an entry either as a Memory project server (remote only) or as a
  locally-hosted connector server (stdio-first).

### Modified Capabilities

- `mcp-servers-ui`: the MCP Servers surface gains a registry browse/install page
  with remote-only install and connector guidance for stdio-only entries.
- `mac-connector-app`: the hosted MCP servers page gains registry browse and a
  prefilled Add-sheet install flow.

## Impact

- **Server** — `apps/server/pkg/sdk/mcpregistry`: proxy methods + DTOs
  (`SearchRegistry`, `GetRegistryServer`); no change to the existing
  `domain/mcpregistry` routes or its remote-only install policy.
- **Connector** — `apps/connector.linux`: new `internal/mcpregistry` translation
  package; `internal/mgmtapi` registry routes; `cmd/memory-connector/relay.go`
  wiring; `internal/memoryapi` registry calls.
- **Mac app** — `apps/connector.mac`: registry client + browse/prefill UI and
  Swift tests.
- **Web UI** — `apps/web-ui/gateway`: client methods, handlers, templ page, and
  sidebar entry.
- No new external dependencies; the official-registry client stays server-side.
