## Context

`ask_user` (`apps/server/domain/agents/ask_user_tool.go:180`) already accepts an optional `proposal` argument — an envelope `{kind, summary, body}` where `body` is opaque JSON server-side. Server validation (`parseProposal`, L432) checks only the envelope shape (non-empty `kind`/`summary`, object `body`) and drops invalid payloads to plain text. The gateway (`proposal.go:49 buildProposalCard`) is the only interpreter today: it renders `kind == "blueprint"` structurally (object/relationship type previews via `objectTypesFromMaps`/`relationshipTypesFromMaps` + `objectTypeRow`/`relationshipTypeRow`), renders any other kind as a summary-only card (L54-57), and diffs against an empty state (L73-74) so every entry reads as an addition.

The operator agent (`blueprints/operator/agents/operator.yaml`) holds the write surface this mechanism is meant to render: `skill-*`, `blueprint-*`, `schema-*`, `agent-def-*` + `update_agent_definition` + `agent-*`, `mcp-server-*` + `update_mcp_server` + `toggle_mcp_server_tool` + `mcp-registry-install`, `provider-*`, `embedding-*`, `token-*`, `document-*`. Its current prompt instructs a structured `proposal` only for blueprint/schema changes and a raw fence for everything else (L100-106).

## Goals / Non-Goals

**Goals**

- Turn the single-kind renderer into a **kind registry** so each new kind is an isolated parser + render pair, with a lossless summary-only fallback.
- Add first-class cards for the operator's remaining writable resources: `skill`, `agent`, `mcp_server`, `provider`, and `object`.
- Update the operator prompt to emit the structured `proposal` for every first-class kind the operator can actually write today.

**Non-Goals (deferred)**

- A real added/changed/removed diff against the project's current state — this change keeps the additions-only summary (the stream/history transforms are pure functions with no project context; threading current state through them is a separate, riskier change).
- Granting the operator graph-write tools (`entity-create`/`relationship-create`) — the `object` card renderer ships here, but the prompt's ability to emit it stays gated on that scope change.
- Re-apply / hydrate a persisted proposal into an actionable apply (still display-only).
- Structural cards for low-complexity resources (`token`, `document`, `embedding_config`) — these stay summary-only/text.
- A new `propose` tool or a parallel pause/SSE/respond pipeline.

## Decisions

### Decision: kind registry in the gateway

Replace `buildProposalCard`'s `switch kind` with a registry:

```go
type proposalKindBuilder func(kind, summary string, body map[string]any) *ProposalCard
var proposalKindBuilders = map[string]proposalKindBuilder{
    "blueprint":  buildBlueprintCard,
    "skill":      buildSkillCard,
    "agent":      buildAgentCard,
    "mcp_server": buildMCPServerCard,
    "provider":   buildProviderCard,
    "object":     buildObjectCard,
}
```

Unknown kinds fall through to the summary-only renderer (kind badge + `summary`), matching today's degradation. A dedicated kind whose body fails to parse or is empty returns `nil` (degrades to plain markdown), matching the blueprint "empty → nil" behavior. The envelope/transport stays unchanged (server `parseProposal` stays envelope-only) and all kind knowledge stays in the gateway.

### Decision: body shapes reuse the apply-path shapes

| kind | body (shape the apply path consumes) | card section |
|---|---|---|
| `blueprint` | `{objectTypes[], relationshipTypes[]}` | object/relationship previews (existing `objectTypeRow`/`relationshipTypeRow`) |
| `skill` | `{name, description, prompt, tools[], bannedTools[]}` | name + description + prompt + tool/banned-tool lists |
| `agent` | `{name, description, model, systemPrompt, tools[], skills[], bannedTools[], flowType, visibility}` | name + model + system prompt + tool/skill/banned-tool lists |
| `mcp_server` | `{name, type, url, headers{}, enabled, enabledTools[], disabledTools[]}` | name + type + url + enable/disable lists |
| `provider` | `{provider, baseUrl, models[]}` (secret never echoed) | slug + base URL + models |
| `object` | `{entities[], relationships[]}` | entity type/key/properties + relationship source→target |

Each body reuses the exact shapes the corresponding write tool consumes, so a proposal never drifts from the apply path (same rationale as the blueprint kind). The provider body excludes the API key — only a masked indicator is rendered (the parser ignores any `apiKey`/`api_key` field outright).

### Decision: `object` kind ships renderer-only

The operator is read-only on the graph today (`entity-type-list`/`entity-query`/… but no `entity-create`/`relationship-create`; guardrail "you never create/update/delete entities"). The `object` card renderer ships here independently of that scope change, and the prompt does not yet instruct emitting `object` proposals — the renderer is harmless (unreachable) until the operator gains the write tools, at which point the prompt is updated in the same change as the scope grant.

### Decision: prompt emits structured proposals for writable kinds

`operator.yaml` "Propose" step (L93-108) generalizes from "blueprint/schema only" to `skill`/`agent`/`mcp_server`/`provider`/`blueprint`: concise markdown summary in `question`, the manifest in `proposal`, and a fenced block only for the remaining low-complexity resources (`token`, `document`, embedding changes). `object` is intentionally not yet listed (gated on the graph-write grant).

## Risks / Trade-offs

- [Gateway couples to more manifest shapes] → each kind reuses the apply-path shapes already imported by the gateway; a shape change propagates automatically (the point).
- [More kinds = more templ to maintain] → registry keeps each kind isolated; the summary-only fallback bounds the cost of adding a kind without a renderer.
- [`object` renderer unreachable until scope grant] → harmless dead code path; the prompt gate keeps it from being emitted before the write tools exist.
- [Diff still additions-only] → explicitly deferred; the card's envelope `summary` carries the human intent in the meantime.
