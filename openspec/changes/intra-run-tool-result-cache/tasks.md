## 1. Cache primitives (`apps/server/domain/mcp/tool_result_cache.go`)

- [ ] 1.1 Define `ToolResultCache` (per-run, mutex-guarded) with an entry map + insertion order, `maxEntries` (64) and `maxBytes` (8 MiB), and `newToolResultCache()`.
- [ ] 1.2 Implement `get` (returns a fresh deep copy by unmarshalling stored bytes) and `put` (marshals once, stores bytes, evicts oldest-first until both bounds hold, skips oversize results).
- [ ] 1.3 Implement `invalidate()` (clear all entries).
- [ ] 1.4 Implement `ContextWithNewToolResultCache(ctx)` / `toolResultCacheFromContext(ctx)`.
- [ ] 1.5 Implement the explicit read-only allowlist (`cacheableReadOnlyTools`) and `isCacheableReadOnlyTool(name)`.
- [ ] 1.6 Implement `canonicalToolCacheKey(projectID, toolName, args, ctx)` with sorted-key canonical JSON + numeric normalisation + trust/namespace auth fingerprint; report a non-cacheable key when args cannot be canonicalised.
- [ ] 1.7 Implement `isCacheableToolResult(ctx, res)` enforcing: non-nil, no dispatch error, live context, not `IsError`, no `ok:false`, no nested `truncated:true`, marshalable.

## 2. Dispatch seam (`apps/server/domain/mcp/service.go`)

- [ ] 2.1 Split the tool-name switch out of `ExecuteTool` into `dispatchTool` (behaviour unchanged).
- [ ] 2.2 Wrap the dispatch in `ExecuteTool` with the cache: non-allowlisted tool → invalidate + dispatch; allowlisted → key lookup, hit returns cached result, miss dispatches then stores only if `isCacheableToolResult`.
- [ ] 2.3 Invalidate the run cache in the early-return hidden-tool branch (`set_session_title`) so its graph patch cannot leave a stale cached read.

## 3. Run wiring (`apps/server/domain/agents/executor.go`)

- [ ] 3.1 Create a fresh cache in `runPipeline` and stamp it into the run context after `runDispatchContext`, so it is visible to every tool call in the run and never inherited by a child run.

## 4. Tests (fail-first)

- [ ] 4.1 `TestExecuteWithToolResultCache_IdenticalReadExecutesOnce` — counting dispatcher: two identical calls → one execution, second returns first result.
- [ ] 4.2 `TestExecuteWithToolResultCache_DifferentArgsMiss` — different args → two executions.
- [ ] 4.3 `TestExecuteWithToolResultCache_MutationInvalidates` — read, mutation (non-allowlisted), read → three executions and second read not served from cache.
- [ ] 4.4 `TestExecuteWithToolResultCache_NoReuseAcrossRuns` — two separate caches do not share entries.
- [ ] 4.5 `TestExecuteWithToolResultCache_NoReuseAcrossProjects` — same cache, different projectID → no hit.
- [ ] 4.6 `TestExecuteWithToolResultCache_ErrorNotCached` — error result then success → second call executes.
- [ ] 4.7 `TestExecuteWithToolResultCache_TruncatedNotCached` — `truncated:true` payload (and `ok:false`) are not stored.
- [ ] 4.8 `TestToolResultCache_Bounds` — entry-count and byte bounds hold; oversize single result not stored.
- [ ] 4.9 `TestToolResultCache_HitReturnsCopy` — mutating a returned result does not corrupt the cached entry.
- [ ] 4.10 Unit tests are the minimum bar; no e2e required for this internal optimisation.

## 5. Verification

- [ ] 5.1 `cd apps/server && go build ./...`
- [ ] 5.2 `go test ./domain/mcp/...` (targeted, no DB required)
- [ ] 5.3 `task lint` (repo lefthook) and `gofmt -l` clean
- [ ] 5.4 `openspec validate intra-run-tool-result-cache --strict`
