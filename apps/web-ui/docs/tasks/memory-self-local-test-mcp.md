# memory-self test MCP for local-docker deployments

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-mcp-servers-ui-tool-calls](../sessions/2026-09-10-mcp-servers-ui-tool-calls.md)

## What

Register Memory's own MCP endpoint (`http://localhost:5300/api/mcp`, `Authorization: Bearer
<emt_ project token>`) as an external MCP server **only in local-docker deployments** where
the backend and gateway share the host.

## Why

It is a deterministic, offline test target (memory's graph/search tools, ~90 tools) for
sync/tool-toggle/agent-attach testing without external network or API keys. It is **not
usable on cloud dev**: the memory process can neither hairpin its own public URL
(`dial tcp <public-ip>:443: connection refused`) nor reach a local `:5300` bind from inside
its deployment.

## Depends on

- none (local `docker-compose` memory stack).

## Notes

- Requires an `emt_` project token whose project matches the gateway session project.
- Watch for tool-name collisions with the `builtin` server (both expose memory tools);
  namespacing is `<slugified server>_<tool>`.
