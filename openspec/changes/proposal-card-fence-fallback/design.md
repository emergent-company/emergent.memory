## Context

`ask_user` (`apps/server/domain/agents/ask_user_tool.go`) accepts an optional `proposal` envelope `{kind, summary, body}`; the gateway (`proposal.go`) renders it as a proposal card, and questions without a proposal render as plain markdown (the spec'd degradation). A stale live agent definition predates the envelope, so its `ask_user` calls paste a blueprint manifest as a ```json fence in the question text — which the gateway shows as an unformatted JSON wall.

## Goals / Non-Goals

**Goals**

- Recover legacy fenced-manifest questions: render a proposal card when the question text embeds a recognizable blueprint manifest and no structured `proposal` is attached.
- Make the structured `proposal` argument discoverable (tool description + blueprint-architect prompt) so agents stop emitting fences.
- Keep the fallback deterministic and testable: first `json`/`yaml`/`yml` fence only, build through the same `blueprint` builder, degrade to markdown otherwise.

**Non-Goals**

- No apply/persist semantics for the fallback — presentation only; the structured envelope remains the canonical path.
- No change to the operator blueprint prompt/version (main already documents the envelope; the live project is stale — ops re-installs).
- No new render models — the fallback reuses the `blueprint` card's `{objectTypes, relationshipTypes}` body shape.

## Decisions

### Decision: fence fallback reuses the `blueprint` builder

`proposalFromQuestionText` extracts the first `json`/`yaml`/`yml` fence, decodes it to `map[string]any`, normalizes a `packs` array (or bare pack) into the `{objectTypes, relationshipTypes}` body shape, and calls `buildBlueprintCard("blueprint", summary, body)`. This guarantees byte-for-byte visual parity with the structured `blueprint` kind — no new templ, no duplicate counts (the chip already reports additions).

### Decision: fence recognition is conservative

Only the first fence whose info string is exactly `json`, `yaml`, or `yml` is considered; bare fences, other languages, unterminated fences, parse failures, and manifests with zero object/relationship types all return nil (question stays markdown). This limits false positives: a stray ```json block that is not a blueprint manifest never renders a card.

### Decision: structured proposal always wins

`fallbackProposalHTML(proposal, question)` renders the structured `proposal` when it produces a card, and only falls back to the question fence otherwise. At the history site the fallback applies only when the tool input has no `proposal` field, so a structured proposal is never overridden or double-rendered.

### Decision: YAML via yaml.v3, JSON via encoding/json

The gateway already depends on `gopkg.in/yaml.v3`, which decodes mappings to `map[string]any`, matching the JSON decode path. Any decode error yields nil (degrade to markdown).

## Risks / Trade-offs

- [False-positive fence] → the conservative recognition (exact language token + manifest shape + non-zero types) bounds this; a non-manifest ```json block never produces a card.
- [Fallback vs structured divergence] → the fallback builds through the same `blueprint` builder, so the two can't diverge visually.
- [Summary is best-effort] → a bare pack without `name` renders a card with an empty summary line; the chip still reports counts, and the question markdown is always visible.
