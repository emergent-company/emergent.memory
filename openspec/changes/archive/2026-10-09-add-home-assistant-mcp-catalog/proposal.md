## Why

Users want the Home Assistant MCP server available through Memory's embedded
curated MCP registry catalog. The entry is present in the official MCP registry
(`io.github.homeassistant-ai/ha-mcp`, v8.6.0, pypi + oci, stdio-only), but it is
absent from GitHub's MCP registry snapshot — the only upstream the catalog
refresh tool currently sources from. Because the catalog is curated from GitHub
slugs alone, Home Assistant MCP cannot be added without also teaching the
refresh tool to read from the official registry.

## What Changes

- **Refresh tool gains a second source kind** (`apps/server/cmd/refresh-mcp-catalog`):
  a source line prefixed `official:` carries an official MCP registry server
  name (resolved at `{registry-base}/v0.1/servers/{name}/versions/latest`) and is
  dispatched separately from GitHub slugs; a new `-registry-base-url` flag
  (default `https://registry.modelcontextprotocol.io`) mirrors the existing
  `-base-url`. Both kinds strip `_meta`, require a non-empty `name`, preserve
  source-list order, and fail without writing on any bad source.
- **Home Assistant MCP entry added** to `sources.txt` and the regenerated
  `servers.json` (28 → 29 entries), last entry `io.github.homeassistant-ai/ha-mcp`.
- **Spec delta** documenting the two-source refresh behavior.

## Known limitation (not a blocker)

The Home Assistant MCP entry is **stdio-only** (pypi + oci packages, both
`stdio` transport). It is therefore browseable and inspectable in the embedded
catalog, but registry install will reject it under Memory's existing
remote-only install policy (no stdio packages are installed server-side, since
they would spawn arbitrary processes on the shared host). Users install it
locally (e.g. `uvx ha-mcp`) instead.
