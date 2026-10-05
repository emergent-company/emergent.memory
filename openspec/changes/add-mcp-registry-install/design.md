# Design — add-mcp-registry-install

## Context

- **Memory server** already integrates the official registry
  (`apps/server/domain/mcpregistry`): `GET /api/admin/mcp-registry/search`,
  `GET /api/admin/mcp-registry/servers/:name`, `POST /api/admin/mcp-registry/install`.
  Install is **remote-only** — stdio packages are explicitly rejected because
  they would spawn arbitrary processes on the shared server host.
- **Connector engine** (`apps/connector.linux`, built `GOOS=darwin` and embedded
  in the Mac app) hosts local MCP servers (`mcphost.ServerConfig`: stdio
  `command/args/env`, or http/sse `url/headers`) and exposes an unauthenticated
  loopback management API (`127.0.0.1:8931`) with CRUD only.
- The connector's config already carries `ServerURL`, project `Token`, and
  `ProjectID`, and `internal/memoryapi` builds a project-scoped SDK client, so
  it can call the admin registry routes.
- **Mac app** currently only talks to the loopback management API; it has no
  registry surface.
- **Web UI gateway** proxies the admin MCP routes but has no registry page.

## Decisions

### 1. The connector proxies the registry through Memory (not the official registry directly)

Chosen: connector loopback endpoints call Memory's `/api/admin/mcp-registry/*`
using the project token.

- One registry client in the codebase; the connector never integrates the
  upstream directly.
- Server-side tests and the hermetic `e2e/stubs/mcp-registry` already cover the
  upstream contract.
- Enables later server-side caching/policy without connector changes.
- Trade-off: registry browsing requires a live Memory connection. Accepted —
  the connector only hosts/relays when connected anyway, and the browse UI is
  only useful from the app, which is signed in.

Rejected: connector calls `registry.modelcontextprotocol.io` directly
(duplicate client, no shared policy); Mac app calls Memory directly and
translates in Swift (translation logic untestable in Go, not reusable).

### 2. Connector suggests stdio-first; Memory stays remote-only

The feature is *local* MCP servers, so the connector prefers a locally-runnable
package over a remote transport:

- npm → `npx -y <identifier>@<version>` (version defaults to the entry's
  version; `-y` avoids the install prompt);
- pypi → `uvx <identifier>` (pinned when a version is present);
- `runtimeArguments` then `packageArguments` appended in registry order;
- `environmentVariables`/headers included as empty placeholders flagged
  required/secret;
- remote fallback (`http` preferred over `sse`) when no supported local package
  exists;
- neither → reported unsupported.

Memory's remote-only install is unchanged; the Web UI degrades stdio-only
entries to connector guidance. This matches the natural split: device =
local/stdio, server = remote.

### 3. Translation lives in Go on the connector; creation reuses the existing endpoint

The loopback `GET /api/mcp-registry/servers/{name}` returns the registry entry
**plus a `suggested` config** derived by the connector. The Mac app prefills the
Add sheet from `suggested`; the user edits; the server is created through the
existing `POST /api/mcp-servers` (validated by `mcphost.ValidateServers`).

- No new "install" write endpoint, so registry installs go through the same
  validated, hot-reloading path as manual ones.
- Translation is unit-testable against `mcphost` rules.
- The suggestion is advisory only: the connector never creates a server itself.

### 4. Runner availability is a warning, not a block

If `npx`/`uvx` is absent, the suggestion still renders with a warning; the user
may install the runner later. Blocking would make the registry unusable while a
runner is being set up and adds host-inspection coupling for little benefit.

### 5. Registry names contain `/`

Registry names (e.g. `io.github.ChromeDevTools/chrome-devtools-mcp`) contain a
slash. The connector must path-escape the name on both its own route and the
upstream Memory call, and the Mac app must pass it encoded.

## Sequence (browse → install)

1. App: `GET {loopback}/api/mcp-registry/search?q=chrome-devtools`
2. Connector: `GET {memory}/api/admin/mcp-registry/search?q=...` (project token)
3. App: user selects → `GET {loopback}/api/mcp-registry/servers/{name}`
4. Connector: `GET {memory}/api/admin/mcp-registry/servers/{name}` → translate →
   `{ server, suggested, envVars, warnings }`
5. App: prefill Add sheet; user edits/fills secrets; `POST {loopback}/api/mcp-servers`
6. Connector: validate, persist, hot-reload; tools become relayable.

## Testing strategy

- **SDK**: table tests for search/get request shape + decode (httptest).
- **Connector translation**: table tests per registry type (npm, pypi, remote,
  unsupported, runner-missing) asserting exact command/args/env/url.
- **Connector mgmtapi**: handler tests with a stub registry source (search
  passthrough, suggestion shape, upstream failure, slash-escaped name).
- **Mac app**: decode + form-prefill unit tests.
- **Web UI**: gateway handler/render tests (remote install path, stdio-only
  guidance), client tests for the new proxy methods.
- e2e: reuse the hermetic `e2e/stubs/mcp-registry` where an end-to-end path is
  cheap; unit tests are the minimum bar.
