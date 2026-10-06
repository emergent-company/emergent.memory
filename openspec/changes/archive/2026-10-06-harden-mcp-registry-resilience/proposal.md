## Why

The official MCP registry (`https://registry.modelcontextprotocol.io`) is slow
and degraded. A search query has been measured returning HTTP 200 only after
~38.5s, and it often hangs beyond 60s. The server's registry client budgets
only 15s per request (`defaultRegistryTimeout`), so every browse/search blows
that budget and the handler returns `502 registry_unavailable` — the
`/settings/mcp-registry` page effectively hangs then fails, and repeated
requests re-hit the slow origin.

## What Changes

- **Longer upstream budget**: bump the registry HTTP client timeout from 15s to
  45s. This covers the ~38.5s observed latency while staying under the
  gateway's 60s client timeout, so a legitimately slow response is returned
  instead of failing every request. (Gateway retries are disabled separately.)
- **Short-TTL in-memory browse cache**: cache successful registry browse results
  (search and single-server lookup) keyed by the full request identity (search:
  query + limit + cursor; lookup: name) with a ~60s TTL. This keeps repeated
  identical queries and pagination from re-hitting the slow origin. Only
  successful results are cached — errors are never cached, so a transient
  upstream failure cannot become sticky.

## Capabilities

### Modified Capabilities

- `mcp-registry`: browse the official MCP registry through Memory now tolerates
  a slow/transient upstream within a bounded budget and serves repeated
  identical browse queries from a short-lived cache, while still surfacing
  failures as errors.

## Impact

- **Server** — `apps/server/domain/mcpregistry`: `defaultRegistryTimeout`
  15s→45s; new in-memory `registryCache` used by `Service.SearchRegistry` and
  `Service.GetRegistryServer`; unit tests for cache hit/miss/expiry/error
  behavior.
- No new external dependencies; the cache is process-local and ephemeral.
- No change to the Web UI, connector, or SDK surfaces.
