# Design — Bound tool results before they re-enter the model context

## Context

An agent run is an ADK `runner` loop. Each tool call produces a
`genai.FunctionResponse{Name, Response}` part which ADK appends to a session
event. On the next model call, ADK's `ContentsRequestProcessor` builds
`model.LLMRequest.Contents` from the session events, **deep-cloning** each
content (`internal/llminternal/contents_processor.go` → `clone`). The
before-model callback registered by `runPipeline`
(`apps/server/domain/agents/executor.go`) runs after `preprocess` populates
`Contents`, and the same `*model.LLMRequest` is then passed to
`Model.GenerateContent`.

This ordering is the key: mutating `llmReq.Contents` in the before-model callback
changes what the model sees and nothing else. The full tool result was already
persisted before the callback (`afterToolCb` → `kb.agent_run_tool_calls`) and
streamed to the UI (`StreamEventToolCallEnd`), and the session event itself is
untouched because ADK cloned it.

## Goals / Non-Goals

**Goals**

- A single central bound for every tool result on the model path, not per-tool
  code.
- Honest truncation: the model always knows a result was cut and how to narrow.
- Configurable, with a generous default that does not disturb normal results.
- Bound only the model context; persisted and streamed payloads stay full.
- No migration.

**Non-Goals**

- No summarisation of tool results (would require a model call at the boundary,
  adds latency and non-determinism).
- No persistence of full results behind a handle for later retrieval; the full
  result is already durably available in `kb.agent_run_tool_calls`, and adding a
  retrieval tool is a larger change.
- No change to the MCP result envelope returned to non-agent callers.
- No bounding of assistant/user text; only tool-result payloads are addressed
  here.

## Decisions

### Where: the before-model callback, mutating `LLMRequest.Contents`

Alternatives considered:

1. **Truncate the map returned by `afterToolCb`.** Simpler, but it would also
   truncate the payload persisted into `kb.agent_run_messages` (the tool-role
   transcript row is derived from the session event, which is built from the
   returned map). Rejected: violates "keep the full persisted payload".
2. **Truncate per tool in `mcp.Service.ExecuteTool`.** Rejected: not central,
   would touch the caller-facing envelope, and does nothing for relay/external
   tools.
3. **Mutate `llmReq.Contents` in `beforeModelCb`.** Chosen. ADK deep-clones the
   contents, so the mutation is confined to the model request. It covers every
   tool backend (builtin, external, relay, workspace) uniformly.

### Per-result envelope: structured marker, not a bare prefix

When a result exceeds its cap, it is replaced with:

```json
{
  "ok": true,
  "truncated": true,
  "truncation": "… [truncated: kept N of M bytes; narrow with filters/fields[]/limit/offset or fetch a specific key]",
  "preview": "<valid-UTF-8 prefix of the original JSON>"
}
```

`ok` is preserved so success/failure classification survives. The marker names
the tool-shape narrowing verbs the graph tools actually accept. The preview lets
the model inspect the start of the payload. The envelope is shrunk iteratively
until its JSON encoding fits the cap, so the bound is strict.

### Total budget: elide oldest, keep most recent

After the per-result pass, the sum of tool-result bytes in the request is
compared against `MCP_TOOL_RESULT_TOTAL_BUDGET_BYTES`. While over budget, the
**oldest** `FunctionResponse` is replaced with:

```json
{"ok": true, "elided": true, "truncation": "… [elided: older tool result (N bytes) omitted to bound the model context; re-run the tool with filters/fields[]/limit/offset if you need it]"}
```

The most recent tool result is never elided: it is what the current step reasons
about. This keeps per-step model input from trending with run length while
preserving the recent working set.

### Config placement

Reuse `internal/config.MCPConfig`, next to `MCP_ENTITY_QUERY_*`:

| Env | Default | Meaning |
|---|---|---|
| `MCP_TOOL_RESULT_MAX_BYTES` | `131072` (128 KiB) | per-result cap |
| `MCP_TOOL_RESULT_MAX_BYTES_OVERRIDES` | `""` | `tool=bytes,tool=bytes` per-tool cap |
| `MCP_TOOL_RESULT_TOTAL_BUDGET_BYTES` | `524288` (512 KiB) | sum across one request |

Zero falls back to the default; a negative value disables that layer. 128 KiB is
generous: normal read/search results observed in production were 11–56 KB and
pass through unchanged; only pathological payloads are cut.

## Risks / Trade-offs

- **The model may need an elided older result.** The marker tells it the result
  existed and how to re-fetch narrowly; this is the standard compaction
  trade-off. The most-recent result is protected.
- **An env misconfiguration could set an absurdly small cap.** The marker
  envelope has fixed overhead; a cap below that overhead returns the
  marker-only envelope (still a few hundred bytes) rather than looping.
- **Preview is a JSON prefix, not parseable JSON.** It is a string, documented
  as a prefix; the marker tells the model the full payload was cut.
