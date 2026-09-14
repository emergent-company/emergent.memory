# Dev Firecrawl MCP API key

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-mcp-servers-ui-tool-calls](../sessions/2026-09-10-mcp-servers-ui-tool-calls.md)

## What

Add a free Firecrawl API key to the dev project's `firecrawl` MCP server as an
`Authorization: Bearer <key>` header, then re-sync.

## Why

Keyless registration exposes only 3 tools (`firecrawl_search/scrape/parse`) and tool
*calls* are rejected ("Anonymous keyless access is unavailable"). A key unlocks the fuller
toolset (`map/crawl/extract`) and lets e2e/QA exercise real tool-call roundtrips — useful
for tool-toggle and agent-tool-call testing beyond Exa's single `web_search_exa`.

## Depends on

- A free key from api-dashboard.firecrawl.dev (human action).
- MCP servers UI (`/settings/mcp-servers` → edit `firecrawl` → add header row → save → sync).

## Notes

- Keep the key out of repo files; store only as the server's header (memory persists it
  plaintext within the tailnet trust boundary — see `docs/spec/09-security.md`).
