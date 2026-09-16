## Why

The `operator` assistant proposes changes (a new blueprint/schema, a skill, an agent definition) by calling `ask_user`. Today it stuffs the full proposal — a JSON/YAML manifest of object types, relationship types, and their typed properties — into the free-text `question` field, which the gateway renders as a monochrome markdown code fence. A blueprint manifest is hundreds of lines of nested JSON; rendered flat it is unreadable, so users cannot effectively review what they are about to accept. The data is already structured end-to-end (the `blueprints` domain owns `ObjectTypeDef`/`RelationshipTypeDef`, and the gateway already renders those exact types into a human-readable preview on the blueprint detail page and schema browser) — it is only flattened to text at the `ask_user` boundary.

## What Changes

- Add an optional structured `proposal` argument to the `ask_user` tool, and persist it as a `jsonb` column on `kb.agent_questions`. The payload is an envelope `{kind, summary, body}`; `body` carries the same manifest types the apply path consumes (`blueprints.ObjectTypeDef`/`RelationshipTypeDef`), so a proposal is not a second, drifting copy of the schema.
- On the gateway, render a question that carries a `proposal` as a **proposal card** instead of a raw code fence: a side-effect summary header (diff against the project's current compiled types), read-only object-type / relationship-type previews (reusing the existing `objectTypeRow`/`relationshipTypeRow`/diff components), and Accept / Reject / Edit actions. Questions without a `proposal` keep rendering as markdown, exactly as today.
- Update the `operator` agent prompt to emit a concise markdown summary plus a structured `proposal` (rather than a raw JSON fence) when it proposes a blueprint/schema change.

## Capabilities

### New Capabilities

- `agent-proposals`: a structured, first-class payload for an agent's `ask_user` checkpoint so the conversation UI can render proposed platform changes (blueprint/schema packs) as a reviewable card instead of raw JSON.

### Modified Capabilities

(none — `ask_user` gains an additive, optional argument; existing plain-text questions and the respond/cancel plumbing are unchanged)

## Impact

- `apps/server/domain/agents/`: `ask_user_tool.go` (new `proposal` arg), `entity.go` (`AgentQuestion.Proposal`), `repository.go` (persist/read `proposal`), DTO + SSE event carry the payload.
- `apps/server/migrations/`: new migration adding `proposal jsonb` to `kb.agent_questions`.
- `apps/web-ui/gateway/`: `sse_markdown.go` (parse + render proposal → `proposalHtml`), reuse of `blueprints.go` parsers + diff helpers and `blueprints.templ`/`schema.templ` preview components; `webui/static/js/chat-stream.js` (inject `proposalHtml`).
- `apps/web-ui/gateway/blueprints/operator/agents/operator.yaml`: prompt update.
- No breaking changes — additive column + optional tool arg; questions without a proposal are byte-for-byte unchanged.
