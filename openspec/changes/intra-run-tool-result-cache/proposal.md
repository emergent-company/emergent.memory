## Why

Within a single agent run nothing prevents the model from issuing the same read-only tool call repeatedly. Dev run `d247a2a1-85f7-40c5-8a87-c4b7c9f171d0` issued two byte-identical `search-knowledge` calls (120s + 61s), four overlapping `entity-search` calls, and five separate `entity-query` fetches — graph reads that are pure functions of the arguments and the current graph state. Tool latency dominated the run (~93% of wall time), so re-running identical queries burns minutes for no new information.

## What Changes

- Add an **intra-run tool-result cache** at the single in-process tool dispatch seam (`mcp.Service.ExecuteTool`). An identical read-only call inside one run executes the underlying query once; subsequent identical calls return the cached result.
- The cache is **per agent run** (created at the top of `runPipeline` and carried in that run's context) — never global, never shared across runs, never inherited by child runs.
- Only tools on an **explicit read-only allowlist** are cacheable. Everything else is executed as today and **invalidates the cache** for the rest of the run, so a cached read can never serve stale data after a mutation.
- Results are **not cached** when they carry an error, when the run context is already cancelled/expired, or when the payload marks itself truncated/incomplete (the #1187 timeout-masking shape).
- `search-knowledge` is deliberately **not** on the allowlist (it spawns a nested agent run that may write the graph, and until #1187 is fixed it can report a timeout as an empty success). Graph search/read tools are the initial allowlist.
- The cache is bounded by entry count and total bytes; eviction is oldest-first.

## Capabilities

### New Capabilities
- `intra-run-tool-result-cache`: per-run, read-only-only memoisation of MCP tool results with write-triggered invalidation, error/timeout exclusion, and bounded memory.

### Modified Capabilities
(none — tool semantics on a cache miss are unchanged; this adds an internal optimisation, not a change to the existing MCP result envelope contract)

## Impact

- `apps/server/domain/mcp/tool_result_cache.go` (new): cache type, context plumbing, canonical key, cacheability predicate, read-only allowlist.
- `apps/server/domain/mcp/service.go`: split the tool dispatch switch into `dispatchTool`; wrap it with the cache in `ExecuteTool`.
- `apps/server/domain/agents/executor.go`: create a fresh cache and stamp it into the run context in `runPipeline`.
- No DB/schema changes, no API changes, no cross-request state. HTTP transports carry no cache in context and therefore keep their current uncached behaviour.
