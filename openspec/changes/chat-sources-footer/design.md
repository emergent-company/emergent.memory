## Context

Citations reach the web client as a small JSON list. Three surfaces render
them today, all through `apps/web-ui/gateway/webui/static/js/chat-components.js`:

- the live stream (`chat.js` / `sidepanel.js` `onCitations`),
- a reloaded transcript (`attachSources(turnEl, item.citations)`), and
- an A2UI `sources` card (`a2uiSources`, agent-supplied items).

The renderer shows `c.type` verbatim, so every surface shows raw schema names.
`attachSources` mounts a sibling `chat chat-start` block, which is the layout
bug.

## Goals / Non-Goals

- **Goals**: one shared source-row renderer; footer inside the assistant bubble;
  collapsed by default; human type labels with a fallback; no regression in
  citation link safety.
- **Non-Goals**: changing the citation derivation or wire payload; iOS/share
  rendering; persisting citations.

## Decisions

### Label resolution: a client type map (not a payload change)

Two options were considered:

- **(a) Enrich the citation payload server-side** with `typeLabel` resolved from
  compiled types. Works for the iOS bridge/share and is server-authoritative,
  but touches `apps/server`, does not cover A2UI `sources` items (agent-supplied
  props that bypass citation derivation), and changes the wire contract.
- **(b) Embed a `name → {label,color}` map in the shell and resolve
  client-side.** Web-UI only; covers every client surface uniformly including
  the A2UI card; degrades to a humanized type name when a type is missing.

**Chosen: (b).** The defect is a rendering defect in the web client, the A2UI
`sources` card cannot be enriched by citation derivation, and (b) keeps the
existing citation wire shape (and therefore the iOS/share consumers) untouched.
The map is derived from the already-available `GetCompiledTypes` projection
(`objectTypeMapFor`) covering object and relationship types.

Trade-off: the map is embedded in the shell by `page()`/`appShell` on every full
page (chat, side panel, A2UI alike), so the humanized fallback fires only when
the type-map fetch errors (empty map) or when a type is absent from the map. Both
are the specified graceful degradation.

### Collapsed disclosure

A native `<details class="collapse collapse-arrow">` (daisyUI 5, the same
pattern as `rawSection` in `chat-components.js` and the spec's citations note)
closed on mount. Native `<details>` gives keyboard/AT semantics for free and
matches `ui.Disclosure`/`ui.Collapsible` server-side. The summary shows
"N sources" (singular-aware).

### Count derivation

The count is the number of rows actually rendered (items with a resolvable
name), so it can never claim a source that is not shown.

### Safety

No `innerHTML` of agent content: names/types/labels are set with `textContent`.
Links still pass through `citationHref` (only `/objects/<uuid>` or a UUID id).
The embedded map is server-derived schema metadata, HTML-escaped by
`encoding/json` before `templ.Raw`, so it cannot break out of the `<script>`.

## DOM contract

```
#chat-messages .chat.chat-start .chat-bubble
  └─ .memory-md
  └─ details[data-testid=chat-sources]            (collapsed; no `open`)
       ├─ summary[data-testid=chat-sources-toggle]
       │    └─ span[data-testid=chat-sources-count]  "N sources"
       └─ .collapse-content
            └─ ul
                 └─ li[data-testid=chat-source]     (one per source)
                      └─ a|span
                           ├─ span[data-testid=chat-source-name]
                           └─ span[data-testid=chat-source-type]   (human label)
```

## Type-map contract

Embedded as `<script type="application/json" id="memory-object-types">`:

```json
{ "LegalParagraph": { "label": "Legal paragraph", "color": "#4F46E5" } }
```

- key: compiled type `name` (object and relationship types merged);
- `label`: `compiledTypeLabel` (falls back to the name server-side);
- `color`: `compiledTypeColor` (omitted when empty).
- Unknown/absent map → client humanizes the raw type name (`LegalParagraph` →
  "Legal paragraph").

## Risks

- An extra best-effort `GetCompiledTypes` call in `page()`; only runs on full
  page renders (partial/boosted renders return earlier), errors degrade to an
  empty map.
