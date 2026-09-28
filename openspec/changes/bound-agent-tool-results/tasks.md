## 1. Bound primitives (`apps/server/domain/agents/tool_result_bounds.go`)

- [x] 1.1 Define `ToolResultBounds` (per-result cap, per-tool overrides, total budget) with default fallbacks (128 KiB / 512 KiB) and negative-disables semantics.
- [x] 1.2 Implement `boundToolResultResponse`: pass through at/under cap; otherwise return a bounded envelope (`ok`, `truncated`, actionable marker, preview) sized strictly under the cap, without mutating the input.
- [x] 1.3 Implement `elidedToolResultResponse`: short, `elided: true` marker preserving `ok`.
- [x] 1.4 Implement `boundModelRequestToolResults`: per-result pass over `FunctionResponse` parts, then oldest-first total-budget elision that never elides the most recent result; return stats.
- [x] 1.5 Implement `parseToolResultMaxBytesOverrides` and `toolResultBoundsFromConfig`.

## 2. Model-boundary wiring (`apps/server/domain/agents/executor.go`)

- [x] 2.1 Add `AgentExecutor.toolBounds`, resolved from `cfg.MCP` in `NewAgentExecutor`.
- [x] 2.2 Apply `boundModelRequestToolResults` in `beforeModelCb` before the model call, with a structured log line when it changes anything.

## 3. Config (`apps/server/internal/config/config.go`, `.env.example`)

- [x] 3.1 Add `MCP_TOOL_RESULT_MAX_BYTES`, `MCP_TOOL_RESULT_MAX_BYTES_OVERRIDES`, `MCP_TOOL_RESULT_TOTAL_BUDGET_BYTES` to `MCPConfig`.
- [x] 3.2 Document the knobs in `.env.example` next to `MCP_ENTITY_QUERY_*`.

## 4. Tests (fail-first)

- [x] 4.1 `TestBoundToolResultResponse_UnderCapUnchanged` — under-cap result returned unchanged.
- [x] 4.2 `TestBoundToolResultResponse_OversizedBoundedWithActionableMarker` — oversized bounded, marker actionable, `ok` preserved, preview present, encoding within cap.
- [x] 4.3 `TestBoundToolResultResponse_DoesNotMutateInput` — source map untouched.
- [x] 4.4 `TestBoundToolResultResponse_NegativeCapDisables` — negative cap disables.
- [x] 4.5 `TestToolResultBounds_MaxBytesFor` / `TestToolResultBoundsFromConfig_DefaultsAndOverrides` / `TestParseToolResultMaxBytesOverrides` — default, override and parse behaviour.
- [x] 4.6 `TestBoundModelRequestToolResults_TruncatesEachOversizedResult` — each oversized result bounded; small result and text part untouched.
- [x] 4.7 `TestBoundModelRequestToolResults_TotalBudgetElidesOldest` — oldest elided, newest retained, sum within budget.
- [x] 4.8 `TestBoundModelRequestToolResults_NeverElidesNewest` — newest never elided.
- [x] 4.9 `TestBoundModelRequestToolResults_NilSafe` — nil/empty request and parts safe.
- [x] 4.10 Unit tests are the minimum bar; no e2e required for this internal bound.

## 5. Verification

- [x] 5.1 `cd apps/server && go build ./...`
- [x] 5.2 `go test ./domain/agents/... ./internal/config/...` (targeted, no DB required)
- [x] 5.3 `task lint` and `gofmt -l` clean
- [x] 5.4 `openspec validate bound-agent-tool-results --strict`
