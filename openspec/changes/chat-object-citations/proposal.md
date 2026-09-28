## Why

An agent answering from the knowledge base returns prose with no traceable
provenance. The identifiers it needs already flow through the run — every graph
tool (`search-hybrid`, `entity-query`, `graph-traverse`, `relationship-list`,
`entity-edges-get`, `search-similar`) returns canonical object and relationship
ids, `ToolPool` hands them to the model verbatim, and `afterToolCb` persists them
to `kb.agent_run_tool_calls`. But nothing turns them back into references the
reader can follow. The only instruction today is a prose nudge ("Cite specific
entity names, types, and relationship types"), which the model satisfies with
names, not links. `kb.chat_messages.citations` and `sse.MetaEvent.Citations`
exist but are never populated.

Meanwhile the A2UI pipeline (`pkg/a2ui` → `StreamEventA2UI` → SSE `ui` →
`MemoryChatComponents.renderA2UISurface`) is live, but the `entity` card renders
plain text (`chat-components.js:796`) and never emits a link — so even structured
cards cannot be followed back to the object.

This change makes answers cite the objects and relationships they actually used:
validated inline links, a Sources block, and an A2UI reference component.

## What Changes

- Derive citations **server-side, from retrieved data** — never from model text
  alone. Walk the run's tool-call outputs for candidate object/relationship ids,
  then keep only those the answer references. This makes a hallucinated id
  impossible to cite.
- **Reference format**: an answer cites an object as `[Label](/objects/<id>)`
  and a relationship as `[A —rel→ B](/objects/<src>#relationship-<rel>)`, using
  the exact canonical ids from tool results.
- **Transport**: a new terminal `citations` SSE event on the live turn, and a
  `citations` field on assistant items in the session timeline / run history, so
  the live turn and a reloaded transcript render identically.
- **Link validation**: a markdown link to `/objects/<id>` whose id is not a
  validated citation renders as plain label text (its `href` stripped); an
  unknown `#relationship-<rel>` fragment is dropped.
- **Web render**: a Sources block under each cited answer, plus the existing
  inline links; both link to the object page.
- **A2UI**: add a `sources` component to the catalog and render it; make `entity`
  link its `id` to `/objects/<id>`.
- **Prompt**: a shared citation instruction for knowledge-base-backed agents.

## Capabilities

### New Capabilities

- `chat-citations`: grounded derivation of object/relationship citations from a
  run's tool outputs, the reference format, the validation rule, the SSE +
  timeline transport contract, the Sources UI, the A2UI reference component, and
  the agent prompt instruction.

### Modified Capabilities

- `web-chat-streaming`: forward the terminal `citations` event and render the
  turn's markdown snapshot with unvalidated object links neutralized.

## Impact

- `apps/server/domain/chat/citations/` (new) — pure candidate extraction,
  answer-reference parsing, validation, and link neutralization.
- `apps/server/domain/chat/handler.go` — emit `citations` before `done` in
  `streamAgentChat`/`QueryStream`.
- `apps/server/pkg/sse/events.go` — `citations` event type.
- `apps/server/domain/agents/repository.go` — citations on
  `ConversationHistoryItem`; `GetConversationFullHistory` populates them.
- `apps/server/domain/agents/executor.go` / `repository.go` /
  `personal_kb_agent.go` — shared citation instruction.
- `apps/server/pkg/a2ui/a2ui.go` — `sources` component (additive).
- `apps/web-ui/gateway/sse_markdown.go` — capture citations per turn, neutralize
  unvalidated links in the snapshot, forward the `citations` event.
- `apps/web-ui/gateway/run_history.go` + `webui/static/js/chat-components.js` +
  `chat.js` / `sidepanel.js` / `chat-host.js` — Sources block and A2UI renderers.
- No migration: citations are a pure function of data already persisted.

## Out of scope (deferred)

- `search-knowledge` (`query_tools.go:70`) returns a synthesized prose answer and
  discards the ids it used; it cannot ground citations until it returns them.
- Document/chunk citations — this change covers graph objects and relationships
  only.
- Persisting citations to `kb.chat_messages.citations`; agent-backed transcripts
  read the run timeline, so nothing needs the column yet.
- iOS rendering of the Sources block.
