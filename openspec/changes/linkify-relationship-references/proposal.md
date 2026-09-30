## Why

Knowledge-base agents are instructed to cite relationships as
`[Source —type→ Target](/objects/<src>#relationship-<rel>)`
(`citationInstruction`, `executor.go`), and the `chat-citations` capability
derives grounded relationship citations from tool output. Two gaps make
relationship references fail to render as links in the web chat:

1. **Derivation misses the real tool shape.** `citations.Candidates` only
   recognises relationships shaped `src_id` + `dst_id`. The KB agent's actual
   `entity-edges-get` output is
   `{entity_id, incoming|outgoing: [{relationship_id, relationship_type, connected_entity}]}`,
   so no relationship candidates are ever collected — no relationship citation
   is ever derived, the `#relationship-<rel>` fragment is always treated as
   unvalidated and dropped, the Sources block never lists relationships, and the
   object-preview `?rel=` ("Referenced via …") context is lost.
2. **Key→id mapping is lost on dedup.** `Derive` keeps the first reference per
   canonical id. When an answer links an object by canonical id first and by its
   human key later (the common form for relationship sources such as
   `lov/1997-06-13-44`), the key reference is dropped and the citation carries no
   `key`, so the renderer cannot re-target key-based links and demotes them to
   plain text.

Separately, agents routinely emit **bare** relationship references in their
answers and thinking blocks — `[#relationship-<uuid>]` or `#relationship-<uuid>`
with no link target — which no renderer step turns into a link.

## What Changes

- Fix `citations.Candidates` to collect relationship candidates from the
  `entity-edges-get` envelope (`entity_id` + `incoming`/`outgoing`), with
  direction: outgoing → source = `entity_id`, target = `connected_entity.id`;
  incoming → reversed.
- Fix `citations.Derive` so a key reference that dedupes against an existing
  citation still records the `key` on that citation.
- Add a web-renderer step that linkifies a **bare** `#relationship-<uuid>`
  reference in answer/thinking text into a grounded link
  `[#relationship-<uuid>](/objects/<src>#relationship-<uuid>)`, using only the
  turn's validated relationship citations (canonical `/objects/<uuid>` source
  page). Truncated ids, references with no matching citation, references already
  inside a link target, and code spans are left untouched; text with no
  relationship citations renders unchanged.

## Capabilities

### Modified Capabilities

- `chat-citations`: adds the bare-reference linkification rule to the render
  contract; the derivation and key-resolution fixes restore behavior the
  capability already specifies ("Derivation covers every graph tool",
  "Key-based references are resolved").

## Impact

- `apps/server/domain/chat/citations/citations.go` — `entity-edges-get`
  envelope handling in `Candidates`; key preservation in `Derive` dedup.
- `apps/web-ui/gateway/markdown.go` — `linkifyRelationshipRefs` +
  `renderCitedMarkdown`.
- `apps/web-ui/gateway/sse_markdown.go` — live snapshot and history render go
  through `renderCitedMarkdown`.
- No API, schema, or event-shape change; the citation payload is unchanged.
