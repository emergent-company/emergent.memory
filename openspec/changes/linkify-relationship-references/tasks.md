# Tasks

Worktree: `/root/emergent.memory-wt/rel-citations` (branch `fix/chat-relationship-citation-links`).

## 1. Derivation — relationship candidates from `entity-edges-get` (Lane A)

- [x] 1.1 `Candidates` recognises the `entity-edges-get` envelope (`entity_id` + `incoming`/`outgoing`); each edge yields `Reference{Kind:"relationship", ID: relationship_id, Type: relationship_type, SrcID, DstID}` with direction resolved from the array
- [x] 1.2 `connected_entity` maps still become object candidates (navigation into the envelope preserved)
- [x] 1.3 Existing `src_id`/`dst_id` rule (e.g. `relationship-list`) unchanged
- [x] 1.4 Relationship refs deduped by id; odd shapes guarded (no panic)
- [x] 1.5 Unit tests: outgoing + incoming direction, `url == /objects/<src>`, connected_entity object candidates

## 2. Derivation — key→id mapping survives dedup (Lane A)

- [x] 2.1 `Derive` records `Key` on an existing citation when a later key reference dedupes against it
- [x] 2.2 Unit test: id-then-key reference carries `Key`

## 3. Render — linkify bare relationship references (Lane B)

- [x] 3.1 `linkifyRelationshipRefs(text, citations)` in `markdown.go`: rewrites bare `#relationship-<full-uuid>` to a grounded markdown link, only for relationship citations whose `url` is a canonical `/objects/<uuid>`
- [x] 3.2 Masks existing link targets, inline code spans, and fenced code blocks before rewriting (no double-wrap, code untouched)
- [x] 3.3 Truncated ids and references without a matching citation left as plain text; no relationship citations → input unchanged byte-for-byte
- [x] 3.4 Shared `renderCitedMarkdown` used by the live snapshot and history render call sites
- [x] 3.5 Unit tests: bracketed + bare refs, truncated ref, existing link target, code span, no-citation unchanged, link survives neutralization

## 4. Verify

- [x] 4.1 `go build ./...` && `go test ./...` in `apps/server`
- [x] 4.2 `go build ./...` && `go test ./...` in `apps/web-ui/gateway`
- [x] 4.3 `openspec validate --changes` clean
- [ ] 4.4 Manual: a KB answer/thinking block with a bare full-uuid `#relationship-…` ref renders a link; truncated refs stay text (dev deploy)

## 5. Ship

- [ ] 5.1 Commit on `fix/chat-relationship-citation-links`, push, `gh pr create --base main`
