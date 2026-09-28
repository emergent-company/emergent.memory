## Why

`search-knowledge` (MCP tool `executeQueryKnowledge`, `apps/server/domain/mcp/query_tools.go`) returns a **successful-looking** result when its budget is exhausted. Observed on dev (chat session `4f161c8f-b79e-4b75-9440-c2aa62da34b1`): a call with `duration_ms=120040` returned `{"ok":true,"answer":"","truncated":true,"session_id":"d69de77e-…"}` while the nested graph-query run it spawned ended `status=failed`, `error_message="Run cancelled: context cancelled"`. The calling agent could not tell "corpus has no answer" from "timed out", re-issued the same tool, and re-did retrieval manually with `entity-query`/`entity-search` — roughly 2 minutes of a ~5.5 minute run wasted.

Two paths mask the failure:

- the SSE loop sets `truncated = true` and returns `ok: true` with an empty answer when `queryCtx.Err() != nil`;
- the `client.Do` error branch has the same `truncated: true` + `ok: true` shape and reports a **stale** `"query timed out after 60s"` string even though the timeout is now 120s (`queryKnowledgeTimeout`, raised for #915).

## What Changes

- On budget exhaustion, `executeQueryKnowledge` no longer returns `ok: true`. A `context.DeadlineExceeded` yields an explicit tool error naming the real budget, derived from `queryKnowledgeTimeout` (never a hardcoded literal).
- A `context.Canceled` is propagated as a cancellation — an error satisfying `errors.Is(err, context.Canceled)` — so it is not misreported as a timeout or swallowed into an empty success.
- The stale `60s` literal is removed from this path; the reported budget is derived from the timeout constant.
- The legitimate `truncated` field is preserved on genuinely successful completions (it remains `false`; there is no partial-success truncation path in this tool today). Only the deadline/cancellation paths stop reporting success.
- The behavior is specified as a new requirement in the `mcp-tool-results` capability.

## Capabilities

### Modified Capabilities

- `mcp-tool-results`: adds a requirement that `search-knowledge` fails loudly (non-success) on budget exhaustion, distinguishes deadline from cancellation, and derives the reported timeout from configuration.

## Impact

- `apps/server/domain/mcp/query_tools.go` — deadline/cancellation handling for both the `client.Do` error branch and the SSE loop; stale literal removed.
- `apps/server/domain/mcp/query_tools_test.go` — fail-first tests for deadline, cancellation, and unchanged success.
- No API/response-shape change for successful calls; failed calls now surface a tool error instead of an empty success.
- Issue: closes #1187. Related: #915, #673.
