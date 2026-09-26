## Why

A real incident surfaced a gap in the proposal-card path: an agent asked for approval via `ask_user` and pasted the proposed blueprint manifest as a ```json fence **in the question text**, because the live agent definition was stale (it predates the structured `proposal` envelope). The gateway renders `questionHtml` (goldmark markdown), so the question card showed an unformatted JSON wall instead of the reviewable proposal card.

The gateway only renders a proposal card when the `ask_user` tool input carries a structured `proposal` envelope. There is no fallback for a fenced manifest in the question text, and the `ask_user` tool description never advertises the `proposal` argument — so an agent pointed at the current tool schema has no way to discover the structured path and falls back to the fence.

## What Changes

- **Gateway fence fallback (presentation-only):** when a question carries no structured `proposal` (or the proposal renders empty), the gateway derives a proposal card from the first `json`/`yaml`/`yml` fence in the question text that decodes to a blueprint manifest (`packs` array or bare pack). It reuses the existing `blueprint` card builder, so the fallback card is visually identical to the structured path. Anything unrecognized still degrades to markdown, exactly as today.
- **Discoverability:** the `ask_user` tool description now advertises the optional `proposal` argument so agents discover the structured path.
- **`blueprint-architect` prompt:** its checkpoints instruct the structured `proposal` (kind `blueprint`, body `{objectTypes, relationshipTypes}`) alongside a short markdown question, never a raw JSON/YAML fence.

The structured envelope remains the primary path; the fence fallback is a legacy-recovery net for stale agent definitions and is presentation-only (no apply/persist behavior changes).

## Capabilities

### New Capabilities

_(none — extends the existing `agent-proposals` capability)_

### Modified Capabilities

- `agent-proposals`: add a fence-fallback render path for legacy questions and advertise the `proposal` argument on the `ask_user` tool.

## Impact

- `apps/web-ui/gateway/proposal.go` — add `proposalFromQuestionText` (fence → card), `renderProposalCardHTML`, and `fallbackProposalHTML`; route `renderProposalHTML` through the shared renderer.
- `apps/web-ui/gateway/sse_markdown.go` — wire the fallback at both injection sites (live `question` event and `renderHistoryHTML`), fallback-only.
- `apps/server/domain/agents/ask_user_tool.go` — extend the tool `Description` (no runtime/validation change).
- `blueprints/blueprint-architect/agents/blueprint-architect.yaml` — instruct the structured `proposal` at the ambiguity + emit checkpoints.
- No breaking changes — structured-proposal behavior and the plain-markdown degradation are unchanged.

## Non-Goals

- No change to the operator blueprint prompt or version (main already documents the envelope; the live dev project is simply stale — ops will re-install).
- No apply/persist semantics for the fallback — it renders a card only; the structured envelope remains the canonical apply path.
