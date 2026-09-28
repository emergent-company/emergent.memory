## Why

`search-knowledge` (MCP tool `executeQueryKnowledge`, `apps/server/domain/mcp/query_tools.go`) returns a **successful-looking** result when its budget is exhausted. Observed on dev (chat session `4f161c8f-b79e-4b75-9440-c2aa62da34b1`): a call with `duration_ms=120040` returned `{"ok":true,"answer":"","truncated":true,"session_id":"d69de77e-…"}` while the nested graph-query run it spawned ended `status=failed`, `error_message="Run cancelled: context cancelled"`. The calling agent could not tell "corpus has no answer" from "timed out", re-issued the same tool, and re-did retrieval manually with `entity-query`/`entity-search` — roughly 2 minutes of a ~5.5 minute run wasted.

Two paths mask the failure:

- the SSE loop sets `truncated = true` and returns `ok: true` with an empty answer when `queryCtx.Err() != nil`;
- the `client.Do` error branch has the same `truncated: true` + `ok: true` shape and reports a **stale** `"query timed out after 60s"` string even though the timeout is now 120s (`queryKnowledgeTimeout`, raised for #915).

A third gap compounds it: the handler never verified that the SSE stream reached its terminal event, so a proxy/backend disconnect could still be assembled into a partial or empty success.

## What Changes

- On budget exhaustion, `executeQueryKnowledge` no longer returns `ok: true`. The expiry is attributed to its cause: the tool budget (`queryKnowledgeTimeout`) or a caller-supplied deadline, whichever expired — never mis-reporting a caller deadline as the internal budget.
- A `context.Canceled` is propagated as a cancellation — an error satisfying `errors.Is(err, context.Canceled)` — so it is not misreported as a timeout or swallowed into an empty success.
- A response stream is only successful once it emitted its terminal event (`{"type":"done"}` chunk or `[DONE]` sentinel). A stream that ends without it — a disconnect, or a deadline before the marker — is an error, not a partial or empty success.
- The stale `60s` literal is removed from this path; the reported budget is derived from the timeout constant.
- The `truncated` field is removed from the success payload and the tool description no longer advertises it; there is no partial-success truncation path in this tool.
- The behavior is specified as a new requirement in the `mcp-tool-results` capability.

## Capabilities

### Modified Capabilities

- `mcp-tool-results`: adds a requirement that `search-knowledge` fails loudly (non-success) on budget exhaustion or an incomplete stream, attributes deadline vs caller-deadline vs cancellation, and derives the reported timeout from configuration.

## Impact

- `apps/server/domain/mcp/query_tools.go` — terminal-event tracking, budget/caller-deadline/cancellation attribution, stale literal removed, `truncated` dropped from the success payload.
- `apps/server/domain/mcp/query_tools_test.go` — fail-first tests for deadline attribution, cancellation, missing terminal event, and unchanged success.
- No API/response-shape change for successful calls apart from removing the dead `truncated: false` field; failed calls now surface a tool error instead of an empty success.
- Issue: closes #1187. Related: #915, #673.
