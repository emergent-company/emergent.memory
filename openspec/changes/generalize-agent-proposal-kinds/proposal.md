## Why

The `ask_user` proposal card only renders one `kind` structurally today — `blueprint` (schema pack). Every other change the `operator` agent can make — a new skill, an agent definition, an MCP server, a provider — still lands in the free-text `question` as a raw JSON/YAML fence, because the operator prompt says "for non-schema changes keep the concrete content in the question text as a fenced block". That leaves the largest share of the operator's writable surface (skill-create, agent-def-create, mcp-server-create, provider-config, …) as unreadable markdown, defeating the point of the proposal card.

## What Changes

- Generalize the gateway proposal renderer from a hardcoded `blueprint` branch into a **kind registry** (`kind → {parse body, render card}`), with a lossless summary-only fallback for any unknown `kind` (today's behavior for non-blueprint kinds).
- Add first-class proposal kinds for the operator's other writable resources, each rendering a reviewable card from the same shapes the apply path consumes:
  - `skill` — new/edited skill (name, description, prompt, tools, banned tools).
  - `agent` — new/edited agent definition (name, description, model, system prompt, tools, skills, banned tools, flow type, visibility).
  - `mcp_server` — register/edit an MCP server (name, type, url, enabled/disabled tool toggles).
  - `provider` — add/edit a provider (provider slug, base URL, models; secret masked).
  - `object` — create graph objects/entities + relationships (renderer ships; prompt emission is gated on the operator's graph-write scope, see Impact).
- Update the `operator` prompt to emit the structured `proposal` argument for every first-class kind the operator can write today (`skill`/`agent`/`mcp_server`/`provider`/`blueprint`), keeping a concise markdown summary in `question`.

## Capabilities

### New Capabilities

(none — this extends the existing `agent-proposals` capability rather than introducing a new one)

### Modified Capabilities

- `agent-proposals`: generalize the single `blueprint` kind into a kind registry with first-class `skill`, `agent`, `mcp_server`, `provider`, and `object` kinds.

## Impact

- `apps/web-ui/gateway/proposal.go` — replace the single `buildProposalCard` with a kind registry; add body parsers + render models for each new kind.
- `apps/web-ui/gateway/proposal.templ` — add card sections per kind (reuse `objectTypeRow`/`relationshipTypeRow` for blueprint; dedicated compact sections for the rest).
- `apps/web-ui/gateway/blueprints/operator/agents/operator.yaml` — prompt update to emit structured proposals for `skill`/`agent`/`mcp_server`/`provider`/`blueprint`.
- No breaking changes — additive kinds; the `blueprint` kind and summary-only fallback keep working; questions without a proposal are unchanged.
- Deferred (separate follow-ups, out of scope here): a real added/changed/removed diff against the project's current state (the card remains additions-only), and granting the operator graph-write tools (`entity-create`/`relationship-create`) so `object` proposals become actionable.
