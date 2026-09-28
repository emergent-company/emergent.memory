## Context

Every in-process MCP tool call from an agent run funnels through one function: `mcp.Service.ExecuteTool` (`apps/server/domain/mcp/service.go`). The ADK `ToolPool` wraps each resolved tool in a closure that calls `svc.ExecuteTool(ctx, projectID, toolName, args)` (`apps/server/domain/agents/toolpool.go`), and direct `CallTool` dispatch uses the same seam. `runPipeline` derives a single run context (`apps/server/domain/agents/executor.go`) and passes it to `runner.Run`, so a value placed in that context is visible to every tool call in the run and to no other run.

Two existing behaviours constrain the design:

- **#1072 per-step watchdog / #1149 cancellation / #1134 run boundary.** A run's context is cancellable and its authority markers are re-derived at the run boundary. A cache must be scoped to exactly one run and must not be inherited by nested runs (which re-enter `runPipeline` with a child context).
- **#1187 timeout masking.** `search-knowledge` currently returns `{"answer":"","truncated":true,...}` with a success-shaped (non-error) payload when its internal 120s budget is exhausted. A cache that keys on "result is not an error" would store that empty payload and serve it to the retry, permanently masking a recoverable timeout.

## Goals / Non-Goals

**Goals**

- Collapse repeated identical read-only tool calls within one run to a single underlying execution.
- Make the correctness story explicit and testable: exact key, exact cacheability, exact lifetime, exact invalidation, exact bounds.
- Never change tool semantics on a miss, never introduce cross-request/global state, never serve a cached read after a mutation.

**Non-Goals**

- No TTL-based caching and no cross-run/conversation caching (staleness surprises).
- No caching of `search-knowledge` until #1187 is fixed and it is reclassified as read-only.
- No caching of external MCP tools, relay tools, workspace tools, coordination tools, or any mutating/admin/session tool.
- No change to the MCP result envelope or to any tool's output shape.

## Decisions

### Cache location and lifetime: per-run, in context

The cache is an in-memory object created fresh in `runPipeline` immediately after `runDispatchContext`, and stamped into the run context via `mcp.ContextWithNewToolResultCache(ctx)`. `ExecuteTool` reads it from context. This gives:

- **lifetime = one run.** The object is unreachable once the run returns; no TTL, no eviction-by-time, no persistence.
- **no cross-request global state.** Nothing is stored on `Service`, in a package var, or in a singleton.
- **no inheritance by child runs.** `runPipeline` always installs a *new* cache, overwriting any cache inherited from a parent run's context. A spawned child therefore starts cold and cannot read its parent's stale entries.
- **HTTP transports unaffected.** They never install a cache, so `ExecuteTool` falls through to an uncached dispatch.

Alternative considered and rejected: closure-captured cache per resolved tool set. It cannot be shared across the built-in closure path and `CallTool`, and it would need a signature change to `ResolveTools` (used by test stubs).

### Key: `(project, tool, canonical args, ambient authorization state)`

```
key = sha256( projectID "\x00" toolName "\x00" canonicalJSON(args) "\x00" authFingerprint )
```

- `projectID` scopes every entry to a tenant; RLS is applied per project, so two projects must never collide.
- `toolName` distinguishes tools that share argument shapes.
- `canonicalJSON` recursively serialises `map[string]any` with sorted keys and normalised numbers (integral floats serialise without a fraction) so semantically identical argument maps produce the same key regardless of Go numeric type. Arguments that cannot be canonicalised (e.g. a func/channel value) make the call uncacheable.
- `authFingerprint` = the run's trust marker plus its namespace. Within one run the principal is fixed, so user isolation is structural (the cache itself is per run); the fingerprint is defence in depth against a principal/namespace change mid-run. The raw token is never read or stored.

Cache hits do not bypass authorization: all authority gates in `ExecuteTool` (share-instance allowlist, superadmin resolution, `authz.AuthorizeTool`) run *before* the cache lookup, on every call. The cache wraps only the tool-specific dispatch switch.

### What may be cached: explicit read-only allowlist

Only tool names on a package-level allowlist are eligible:

`entity-query`, `entity-search`, `entity-history`, `entity-type-list`, `entity-edges-get`,
`relationship-list`, `tag-list`, `search-hybrid`, `search-semantic`, `search-similar`,
`graph-traverse`, `schema-version`, `schema-list`, `schema-get`, `schema-compiled-types`.

These are direct reads of graph/schema state with no write side effect and no nested agent run. Any tool *not* on the allowlist is executed normally and treated as a potential mutation (see invalidation).

### Invalidation: invalidate on any non-allowlisted tool

Before dispatching any non-allowlisted tool, the cache is cleared for the remainder of the run. Cleared on both success and failure: a failed write can still have partial effects, and the cost of clearing is only a cache miss. This is deliberately coarse — correctness over hit rate. It also covers hidden tools that return before the dispatch switch (e.g. `set_session_title`, which patches a graph object) via an explicit invalidate in that branch.

A failed *cacheable read* (Go error) does **not** clear the cache: a read that failed cannot have changed graph state, so existing entries are still valid.

### Bounding: entry count + total bytes, oldest-first eviction

Defaults: `maxEntries = 64`, `maxBytes = 8 MiB`. Each entry stores the marshalled result bytes (so its size is known and hits return a fresh deep copy via `json.Unmarshal`, eliminating shared-mutable-state aliasing between callers). On insert, oldest entries are evicted until both bounds hold; a single result larger than `maxBytes` is not cached.

### Non-cacheable results

A result is stored only when **all** hold:

1. Dispatch returned no Go error and a non-nil result.
2. `ctx.Err() == nil` after dispatch (no timeout/cancellation occurred around the call).
3. `result.IsError == false`.
4. The structured content does not have top-level `ok: false`.
5. The structured content has no `truncated: true` at any depth (covers #1187's `search-knowledge` timeout shape and truncated traversals — an incomplete payload must never be reused as if complete).
6. The result marshals to JSON successfully.

### Deliberately not cached (and why)

- **Mutating tools** (`entity-create`, `entity-update`, `entity-delete`, `relationship-create`, `*` `schema-assign/create/delete`, `graph-branch-merge`, `blueprint-apply`, `remember`, `forget`, `queue-reextraction`, `finalize-discovery`, …) — they change state; caching them would be wrong and they instead invalidate the cache.
- **`search-knowledge`** — spawns a nested agent run, which may itself write the graph (so it is not read-only), and its #1187 timeout result is success-shaped but empty. Excluded until #1187 is fixed and its nested-write behaviour is bounded.
- **External MCP / relay / workspace / coordination tools** — dispatch does not pass through this seam for all of them, their idempotency is unknown, and relay/workspace calls have side effects on a device/sandbox.
- **Session/run-inspection reads** (`agent-run-*`, `session-*`, `journal-*`) — they read state that the current run itself mutates as it progresses (tool-call rows, session todos), so a within-run repeat legitimately returns different data.
- **Web tools** (`web-search-*`, `web-fetch`) — non-deterministic external content; excluded for a tight correctness story.
- **Admin/provider/token tools** — not read-only at the level this cache reasons about, and not on the hot path.

### Interaction with #1187

Rule 5 means a timeout-shaped empty success is never cached, so a retry after a timeout re-executes rather than hitting a poisoned entry. Excluding `search-knowledge` from the allowlist additionally avoids caching any of its success-shaped results until #1187 lands. When #1187 makes timeouts explicit errors, `search-knowledge` can be reconsidered (it would then satisfy rule 3, but still needs the nested-write question settled).

## Risks / Trade-offs

- **A read tool with an undeclared side effect** could be cached and hide that side effect. Mitigation: the allowlist is explicit and small; each entry is a direct graph/schema read reviewed at the allowlist site.
- **A mutation outside `ExecuteTool`** (e.g. a nested run's write, or an async extraction) could leave a stale cached read. Mitigation: nested runs install their own cache and the tool that spawns them (`trigger_agent`, `search-knowledge`) invalidates the parent cache / is excluded; async extraction is triggered by `queue-reextraction`/`finalize-discovery`, which are non-allowlisted and invalidate.
- **Memory growth** — bounded by `maxEntries` + `maxBytes` per run and freed when the run returns.

## Migration Plan

Additive. No schema, API, or config change. Revert is deleting the wrapper call; behavior returns to the status quo.

## Open Questions

- Whether to extend the allowlist to more pure reads (e.g. `document-list`, `skill-list`, `provider-models-list`) — intentionally deferred; the initial set covers the reported hot path.
- Whether a follow-up should cache across tool *rounds* within one model step using an explicit `truncated`-as-error fix from #1187 — out of scope here.
