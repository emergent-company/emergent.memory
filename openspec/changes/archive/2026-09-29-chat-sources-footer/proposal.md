## Why

The chat "Sources" widget renders as a sibling `chat chat-start` row under each
assistant answer, so it sits on the agent side as its own message: it distorts
the assistant bubble, is always expanded and bulky, and shows the raw schema
type name (`LegalParagraph`) next to each source, which reads like a legal
paragraph number rather than a human object type. The same raw type and the same
oversized block also appear in the side panel and the A2UI `sources` card.

The fix is a single, reusable sources renderer that folds citations into the
assistant bubble as a quiet, collapsed footer and shows the schema's human
label (falling back to a humanized type name) instead of the machine name.

## What Changes

- **Footer inside the bubble.** The sources disclosure is inserted INSIDE the
  assistant `.chat-bubble`, after `.memory-md`, instead of being a sibling
  `chat chat-start` node. It still reads as agent-side content.
- **Collapsed by default.** A daisyUI `collapse collapse-arrow` `<details>`
  disclosure, closed on mount, whose summary reads "N sources"
  (singular "1 source").
- **Compact source rows.** Each source renders as one compact, linkable box in
  the spirit of the existing entity row: human type badge + name + object link.
- **Human type labels.** The raw schema type is replaced by the compiled type's
  `label` (`LegalParagraph` → "Legal paragraph"), with a humanized-type-name
  fallback when the type is unknown. A `name → {label,color}` map is
  embedded in the shell so the client resolves labels synchronously.
- **One renderer everywhere.** The live SSE path, history replay, side panel,
  and the A2UI `sources` card all use the same row builder and label resolver.
- **Safety unchanged.** Labels/types stay `textContent`; object links remain
  gated by the existing `citationHref` rule (`/objects/<uuid>` or UUID id only).
- The A2UI `sources` card keeps the same visual language (compact rows) as the
  in-bubble footer.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `chat-citations`: the "Sources are surfaced to the user" requirement changes —
  the Sources UI is a collapsed footer inside the assistant bubble, each source
  is a compact linkable row showing the human type label (humanized fallback),
  and the renderer is shared across every citation surface. The A2UI `sources`
  component requirement is extended to use the same row language.

## Impact

- `apps/web-ui/gateway/type_ui.go` — `objectTypeUI` (label + color) and
  `objectTypeMapFor`, mapping both object and relationship compiled types.
- `apps/web-ui/gateway/ui.templ` — embed the map as a `<script
  type="application/json" id="memory-object-types">` in the shell.
- `apps/web-ui/gateway/ui.go` — `page()` builds the map (best-effort).
- `apps/web-ui/gateway/webui/static/js/chat-components.js` — replace the
  sibling Sources block with the in-bubble footer + shared source-row renderer;
  update `attachSources` and the A2UI `sources` card.
- `apps/web-ui/gateway/webui/static/js/chat.js` / `sidepanel.js` — unchanged
  call sites (`attachSources` keeps its signature).
- `docs/spec/12-ai-chat.md` — citations section updated.
- No server (memory) wire change; no migration.
