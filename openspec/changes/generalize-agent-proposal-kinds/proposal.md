## Why

The `ask_user` proposal card only renders one `kind` structurally today — `blueprint` (schema pack). Every other change the `operator` agent can make — a new skill, an agent definition, an MCP server, a provider — still lands in the free-text `question` as a raw JSON/YAML fence, because the operator prompt says "for non-schema changes keep the concrete content in the question text as a fenced block". That leaves the largest share of the operator's writable surface (skill-create, agent-def-create, mcp-server-create, provider-config, …) as unreadable markdown, defeating the point of the proposal card. Separately, even for `blueprint` the card diffs against an empty state, so a *change* to an existing type is mislabeled "adds" — the "changed / removed" half of the summary is unimplemented.

## What Changes

- Generalize the gateway proposal renderer from a hardcoded `blueprint` branch into a **kind registry** (`kind → {parse body, render card}`), with a lossless summary-only fallback for any unknown `kind` (today's behavior for non-blueprint kinds).
- Add first-class proposal kinds for the operator's other writable resources, each rendering a reviewable card from the same shapes the apply path consumes:
  - `skill` — new/edited skill (name, description, prompt, tools, banned tools).
  - `agent` — new/edited agent definition (name, description, model, system prompt, tools, skills, banned tools, flow type, visibility).
  - `mcp_server` — register/edit an MCP server (name, type, url, headers, enabled/disabled tool toggles).
  - `provider` — add/edit a provider (provider slug, base URL, models; secret masked).
  - `object` — create graph objects/entities + relationships (a new kind; see scope note below).
- Compute the proposal card's side-effect summary against the project's **current state** (compiled types, existing agents/skills/MCP servers), so the header reports **added / changed / removed** counts — not just additions. This fixes the additions-only stub in `buildProposalCard` (`diffObjectTypes(nil, …)`).
- Update the `operator` prompt to emit the structured `proposal` argument for all of the above kinds (not only blueprint/schema), keeping a concise markdown summary in `question`.
- Extend the `operator` agent's scope to **write graph objects** (entity-create/relationship-create) so `object` proposals are actually actionable, or explicitly gate `object` behind that scope change.

## Capabilities

### New Capabilities

(none — this extends the existing `agent-proposals` capability rather than introducing a new one)

### Modified Capabilities

- `agent-proposals`: generalize the single `blueprint` kind into a kind registry with first-class `skill`, `agent`, `mcp_server`, `provider`, and `object` kinds, and make the side-effect summary a real added/changed/removed diff against current state.

## Impact

- `apps/web-ui/gateway/proposal.go` — replace the single `buildProposalCard` with a kind registry; add body parsers + render models for each new kind; thread the project's current state into the diff.
- `apps/web-ui/gateway/proposal.templ` — add card sections per kind (reuse `objectTypeRow`/`relationshipTypeRow`, and the existing skill/agent/MCP-server detail rows).
- `apps/web-ui/gateway/sse_markdown.go` — pass project context into `renderProposalHTML` for the SSE rewrite and history rehydration paths.
- `apps/web-ui/gateway/blueprints/operator/agents/operator.yaml` — prompt update to emit structured proposals across kinds; scope expansion for graph-object writes.
- No breaking changes — additive kinds; the `blueprint` kind and summary-only fallback keep working; questions without a proposal are unchanged.
