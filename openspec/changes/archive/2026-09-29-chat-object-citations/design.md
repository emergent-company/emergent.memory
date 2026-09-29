## Context

Agent-backed chat answers come from `streamAgentChat` (`chat/handler.go`). The
run's tool calls persist to `kb.agent_run_tool_calls` (`AgentRunToolCall.Output`
is the LLM-facing result map, ids intact); `result.RunID` is in scope where the
turn ends, and `(*agents.Repository).FindToolCallsByRunID` (`repository.go:1878`)
reads them. History for an ACP-backed conversation is served from the run timeline
by `GetConversationFullHistory` (`repository.go:3233`) → `ConversationHistoryItem`
(`repository.go:3206`); `kb.chat_messages` assistant rows are ignored there.
Live transport is SSE through the gateway rewriter (`sse_markdown.go`), which
already special-cases `done` to emit one sanitized markdown snapshot.

## Goals / Non-Goals

**Goals**

- Cites only what the run's tools returned and the answer referenced.
- One derivation, used by both the live turn and every history/reload path.
- Additive: no migration, no change to the markdown/token stream for turns with
  no citations.

**Non-Goals**

- Document/chunk citations; `search-knowledge` grounding; iOS rendering;
  persisting to `kb.chat_messages.citations`.

## Decisions

### D1 — Citations are derived at read time, not persisted

A citation list is a pure function of `(tool-call outputs, answer text)`. Both
inputs are already persisted, so the server computes citations wherever the
answer is served — no new column, no write-path coupling, and existing runs gain
citations retroactively. A write-path cache can be added later behind the same
payload shape.

### D2 — Grounding rule: candidates ∩ references

1. **Candidates** — walk each tool-call `Output` generically:
   - *object*: a map with a UUID string `id` **and** a string `type`;
   - *relationship*: a map with `src_id` and `dst_id`.
   The walk is shape-agnostic so it survives `slim*` changes and covers every
   graph tool without a per-tool parser. Ids are canonical by construction
   (`response_contract.go` emits canonical ids and never physical ids).
2. **References** — parse the answer for `/objects/<uuid>` links (markdown and
   bare) and `#relationship-<uuid>` fragments, plus A2UI component `id`s.
3. **Validate** — a reference is a citation iff its id is in candidates.
   Candidates not referenced are *not* cited (the user asked for "only what the
   agent actually used").

### D3 — Wire shape

```go
type Citation struct {
    Kind  string `json:"kind"`  // "object" | "relationship"
    ID    string `json:"id"`
    Type  string `json:"type"`  // object schema type, or relationship type
    Label string `json:"label"` // object name; "A —rel→ B" for a relationship
    URL   string `json:"url"`   // "/objects/<id>"; relationship → source page
}
```

Live: the server emits `{"type":"citations","citations":[…]}` once, after the
last token and before `done`. History: `ConversationHistoryItem.Citations` on
`assistant_message` items. Same struct in both.

### D4 — Neutralization happens where the HTML is produced

The gateway already renders exactly one sanitized snapshot per turn. It captures
the turn's citations from the `citations` event (which precedes `done`), then
renders the snapshot with `/objects/<id>` links whose id is not a citation
demoted to plain text and unknown `#relationship-<rel>` fragments dropped. The
history renderer (`renderHistoryHTML`) applies the same rule using each item's
citations. Keeps one sanitization boundary and makes live and history identical.

### D5 — A2UI is additive

`sources` (required prop `items`) joins `BasicCatalog`; `a2uiEntity` gains a link
on `id`. Both are additive per the `agent-structured-ui` catalog rule — no
envelope change, unknown-component fallback still covers older clients.

## Risks / Trade-offs

- **Model must emit links.** Without a link, a retrieved-but-unlinked object is
  not cited. Mitigated by the prompt instruction; accepted because it is what
  makes "only what the agent used" precise.
- **Id-shape walk may miss exotic shapes.** A shape missing both `id`+`type` is
  skipped (no false citations); a new tool emitting that shape is still covered.
- **Generic walk could over-collect** (e.g. a nested object with an id). The
  reference-intersection in D2.3 prevents over-citing.

## Verification

- `citations` package unit tests: candidates from each tool shape; validation
  keeps retrieved+referenced, drops hallucinated; link neutralization.
- `go test ./...` in `apps/server` and `apps/web-ui/gateway`.
- `templ generate` + gateway `task lint`; `node --check` on touched JS.
- `openspec validate --changes`.
