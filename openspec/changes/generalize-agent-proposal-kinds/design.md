## Context

`ask_user` (`apps/server/domain/agents/ask_user_tool.go:180`) already accepts an optional `proposal` argument — an envelope `{kind, summary, body}` where `body` is opaque JSON server-side. Server validation (`parseProposal`, L432) checks only the envelope shape (non-empty `kind`/`summary`, object `body`) and drops invalid payloads to plain text. The gateway (`proposal.go:49 buildProposalCard`) is the only interpreter today: it renders `kind == "blueprint"` structurally (object/relationship type previews via `objectTypesFromMaps`/`relationshipTypesFromMaps` + `objectTypeRow`/`relationshipTypeRow`), renders any other kind as a summary-only card (L54-57), and diffs against an empty state (L73-74) so every entry reads as an addition.

The operator agent (`blueprints/operator/agents/operator.yaml`) holds the write surface this mechanism is meant to render: `skill-*`, `blueprint-*`, `schema-*`, `agent-def-*` + `update_agent_definition` + `agent-*`, `mcp-server-*` + `update_mcp_server` + `toggle_mcp_server_tool` + `mcp-registry-install`, `provider-*`, `embedding-*`, `token-*`, `document-*`. Its current prompt instructs a structured `proposal` only for blueprint/schema changes and a raw fence for everything else (L100-105).

## Goals / Non-Goals

**Goals**

- Turn the single-kind renderer into a **kind registry** so each new kind is an isolated parser + render pair, with a lossless summary-only fallback.
- Add first-class cards for the operator's remaining writable resources: `skill`, `agent`, `mcp_server`, `provider`, and `object`.
- Compute the side-effect summary against the project's **current state** so the header reports added/changed/removed, not just additions.
- Update the operator prompt to emit the structured `proposal` for every first-class kind.

**Non-Goals**

- Re-apply / hydrate a persisted proposal into an actionable apply (still display-only).
- Structural cards for low-complexity resources (`token`, `document`, `embedding_config`) — these stay summary-only/text.
- A new `propose` tool or a parallel pause/SSE/respond pipeline.

## Decisions

### Decision: kind registry in the gateway

Replace `buildProposalCard`'s `switch kind` with a registry:

```go
type proposalKind struct {
    parse func(json.RawMessage) (renderModel, error) // body → model
    render func(renderModel) templ.Component           // model → card section
}
var proposalKinds = map[string]proposalKind{ "blueprint": …, "skill": …, … }
```

Unknown kinds fall through to the summary-only renderer (kind badge + `summary`), matching today's degradation. A kind whose body fails to parse falls back to summary-only (never an error). This keeps the envelope/transport unchanged (server `parseProposal` stays envelope-only) and confines all kind knowledge to the gateway.

### Decision: body shapes reuse the apply-path structs

| kind | body (manifest shape the apply path consumes) | gateway structs reused |
|---|---|---|
| `blueprint` | `{objectTypes[], relationshipTypes[]}` (+ optional `agents[]`, `skills[]`, `seed[]`) | `objectTypesFromMaps`/`relationshipTypesFromMaps`, `objectTypeRow`/`relationshipTypeRow` |
| `skill` | `{name, description, prompt, tools[], bannedTools[]}` | skill detail row |
| `agent` | `{name, description, model, systemPrompt, tools[], skills[], bannedTools[], flowType, visibility}` | `BundledAgent` + `agentDetailRow` |
| `mcp_server` | `{name, type, url, headers{}, enabled, enabledTools[], disabledTools[]}` | MCP-server row |
| `provider` | `{provider, baseUrl, models[]}` (secret masked, never echoed) | provider row |
| `object` | `{entities[], relationships[]}` | graph/entity preview (new section) |

Each body reuses the exact shapes the corresponding write tool consumes, so a proposal never drifts from the apply path (same rationale as the blueprint kind). The provider body explicitly excludes the API key — only a masked indicator is rendered.

### Decision: diff against current state

The renderer currently diffs `nil`. To report added/changed/removed, the gateway must supply the project's current state (compiled types, agent definitions, skills, MCP servers). `renderProposalHTML` is a pure function today; make it accept a `proposalContext` (current state snapshot) that the caller fetches:

- `sse_markdown.go` (`rewriteChatStream`) — currently has no project handle; the stream rewrite is scoped to the conversation, so the server must pass a per-conversation context (or the question event is enriched where project context exists).
- `run_history.go` (`renderHistoryHTML` + `getRunHistory`) — has `s.memory` + project/session context.

Where current state is unavailable (e.g. an unauthenticated stream edge), the diff degrades to additions-only exactly as today. `proposalSummaryLabel` already supports added/changed/removed counts (L115-139); the fix is feeding it non-empty `before` state.

### Decision: `object` kind requires an operator scope change

The operator is read-only on the graph today (`entity-type-list`/`entity-query`/… but no `entity-create`/`relationship-create`; guardrail "you never create/update/delete entities"). `object` proposals are only actionable if the operator gains those write tools + a revised guardrail (propose-then-write, same checkpoint contract). Ship the `object` card renderer independently of the scope change, and gate the prompt's ability to emit it on the tool grant — the renderer is harmless (summary-only) if the operator never emits `object`.

### Decision: prompt emits structured proposals for all kinds

`operator.yaml` "Propose" step (L93-108) generalizes from "blueprint/schema only" to "every first-class kind": concise markdown summary in `question`, the manifest in `proposal` for `skill`/`agent`/`mcp_server`/`provider`/`object`/`blueprint`, and a fenced block only for the remaining low-complexity resources (`token`, `document`, embedding changes).

## Risks / Trade-offs

- [Gateway couples to more manifest shapes] → each kind reuses the apply-path structs already imported by the gateway; a shape change propagates automatically (the point).
- [Diff needs project context the stream rewrite lacks] → thread a context object; degrade to additions-only where absent (matches current behavior).
- [`object` scope change widens the operator's blast radius] → keep propose-then-write contract; renderer ships first, prompt emission gated on the tool grant.
- [More kinds = more templ to maintain] → registry keeps each kind isolated; the summary-only fallback bounds the cost of adding a kind without a renderer.
