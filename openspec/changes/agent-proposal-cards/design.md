## Context

The `ask_user` tool (`apps/server/domain/agents/ask_user_tool.go:180`) is the single proposal checkpoint for the operator agent. It parses `{question, options, interaction_type, placeholder, max_length}` (L187-251), persists an `AgentQuestion` row (`kb.agent_questions`, model `entity.go:469-492`) via `CreateAndEmitQuestion` (L115), and emits an SSE event (`emitQuestionSSEEventDirect`, L347). The gateway (`sse_markdown.go:51`) rewrites the `ask_user` MCP tool call into a `question` SSE event (L86-127), `renderMarkdown`s the question into `questionHtml` (L109, `markdown.go` goldmark + bluemonday), and the browser injects that HTML into the card (`chat-stream.js:469`). A JSON manifest in the question therefore renders as a monochrome `<pre><code>` — no syntax highlighting is applied to markdown fences (`highlight.go` only colors tool I/O).

Structured renderers already exist server-side: `blueprints.go:465-560` (`BlueprintDetail`/`ObjectTypeDetail`/`PropertyDetail`), the manifest parsers `objectTypesFromMaps`/`relationshipTypesFromMaps`/`propertiesFromMap` (`blueprints.go:722-772`), the diff helpers `diffObjectTypes`/`diffRelationshipTypes` (`blueprints.go:941-1010`), and the templ previews `objectTypeRow`/`objectTypeProperties`/`relationshipTypeRow`/`compiledTypesTable` (`blueprints.templ`, `schema.templ`). The `blueprints` domain owns the canonical manifest types (`apps/server/domain/blueprints/manifest.go`).

## Goals / Non-Goals

**Goals:**

- Let the agent attach a typed proposal to `ask_user` so the UI can render it structurally, not as text.
- Reuse the existing blueprint/schema preview + diff machinery so the proposal card matches the post-apply UI.
- Keep plain-text questions unchanged (backward compatible) and the respond/cancel plumbing untouched.
- Keep the proposal payload the same structs the apply path consumes (no drift).

**Non-Goals:**

- A new "propose" tool or a parallel pause/SSE/respond pipeline (ask_user already owns it).
- Re-apply / re-hydrate a persisted proposal from history into an actionable apply (v1 is display-only; `proposalVersion`/re-apply is deferred).
- Rendering arbitrary free-form proposal bodies for every future `kind` — v1 renders `blueprint` (schema pack) structurally and falls back to a generic summary for unknown kinds.

## Decisions

**Decision: extend `ask_user`, do not fork a `propose` tool.**
`ask_user` already owns pause/notification/SSE/respond/history; a dedicated tool duplicates it. Add an optional `proposal` argument (jsonb) alongside `question`.

**Decision: `proposal` envelope = `{kind, summary, body}`; `body` uses the `blueprints` manifest structs.**
The envelope lives in the agents domain next to `AgentQuestion`, but its `body` fields are the `blueprints.ObjectTypeDef`/`RelationshipTypeDef` structs (or their jsonb form) — one source of truth with the apply path. `kind` is a string discriminator; the v1 first-class `kind` is `blueprint` (object types + relationship types). Unknown kinds render losslessly (summary + formatted body), never error.

**Decision: persist `proposal jsonb` on `kb.agent_questions`, additive migration.**
Nullable column; `AgentQuestion.Proposal json.RawMessage` (or `map[string]any`), mapped through `AgentQuestionDTO` and the SSE event. The gateway parses it from the question event.

**Decision: validate at the tool boundary, degrade to text.**
`ask_user` coerces `proposal` against the envelope/manifest shape; on any parse or validation failure it drops the structured payload and proceeds as a plain-text question — no mid-run hard error.

**Decision: gateway renders server-side and ships `proposalHtml`.**
`sse_markdown.go` gains: parse `proposal` → `Proposal{Kind, Summary, Body}` → `objectTypesFromMaps`/`propertiesFromMap` → `diffObjectTypes`/`diffRelationshipTypes` vs current compiled types → render a card (summary header + `objectTypeRow`/`relationshipTypeRow` + diff badges) via templ → `proposalHtml`. Same channel as `questionHtml`; the browser stays dumb (`chat-stream.js` injects `proposalHtml` above the options). No `proposal` → `questionHtml` markdown as today.

**Decision: prompt change — emit markdown summary + structured `proposal`.**
`operator.yaml` "Propose" step: put a concise human summary in `question` (not a raw JSON fence) and the manifest in `proposal`. This is the quick win and the source of the structured payload.

## Risks / Trade-offs

- [Manifest shape varies (array vs map `objectTypes`; unknown `kind`s)] → parsers already tolerate both (`objectTypesFromRaw`); unknown `kind` → generic summary fallback; validation at the tool boundary drops only the structured payload, never the question.
- [Gateway couples to `blueprints` manifest shape] → the payload reuses the same structs; a manifest change propagates to proposals automatically (that is the point). The gateway already imports these parsers for the blueprint detail page.
- [Diff against current compiled types may be empty if the project has no schema yet] → the diff helpers already handle empty/absent current state (used by blueprint compare); render "adds N object types" from the proposal body when there is no prior state.
- [Older clients ignore `proposalHtml`] → the card still renders `questionHtml` markdown as fallback, so nothing breaks; `proposalHtml` is additive.
