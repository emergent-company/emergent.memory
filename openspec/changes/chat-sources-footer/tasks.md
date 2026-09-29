# Tasks

Worktree: `/root/emergent.memory-wt/chat-sources-footer` (branch `feat/chat-sources-footer`).

## 1. Gateway — type map

- [x] 1.1 `type_ui.go`: `objectTypeUI{Label,Icon,Color}` + `objectTypeMapFor(objectTypes, relationshipTypes []CompiledType) map[string]objectTypeUI` (skips shadowed/empty names); `objectTypeMapJSON` serializer (`encoding/json`, HTML-escaped so it cannot break out of a `<script>`); `objectTypeMapScript` (templ treats `<script>` bodies as raw text, so the element is emitted raw)
- [x] 1.2 `ui.templ`: emit `<script type="application/json" id="memory-object-types">` in the shell
- [x] 1.3 `ui.go page()`: build the map best-effort from `GetCompiledTypes`; empty map on error
- [x] 1.4 Unit tests: map includes label/icon/color, merges relationship types, skips empty/shadowed names; serializer round-trips and escapes `<`/`>`/`&`; shell renders the script for non-empty and empty maps

## 2. Web — shared sources renderer

- [x] 2.1 `chat-components.js`: `humanizeTypeName`, `objectTypeLabel`, `objectTypeAccent` (map lookup + humanized fallback)
- [x] 2.2 `sourceRow` + `sourceRows` (compact, linkable entity-row-style boxes; `data-testid=chat-source`, `chat-source-name`, `chat-source-type`; `textContent` only; `citationHref` link rule)
- [x] 2.3 `sourcesFooter(citations)` — `<details class="collapse collapse-arrow">` closed by default, summary "N sources" (`data-testid=chat-sources`, `chat-sources-toggle`, `chat-sources-count`)
- [x] 2.4 `attachSources` inserts the footer INSIDE `.chat-bubble` after `.memory-md`; idempotent; no sibling `chat chat-start` node
- [x] 2.5 A2UI `a2uiSources` uses the same `sourceRows` + count
- [x] 2.6 `node --check` on `chat-components.js`

## 3. Docs / spec

- [x] 3.1 Update `docs/spec/12-ai-chat.md` citations section (in-bubble collapsed footer, human label)
- [x] 3.2 Delta spec + proposal + design under `openspec/changes/chat-sources-footer/`

## 4. E2E

- [x] 4.1 Playwright spec `specs/sessions/chat-sources-footer.spec.ts` asserting collapsed-by-default footer, expand, and the `chat-source*` testids (deterministic, no LLM: builds a bubble in-page and calls `attachSources`)

## 5. Verify

- [x] 5.1 `templ generate` (web-ui)
- [x] 5.2 `go build ./...` && `go test ./...` in `apps/web-ui/gateway`
- [x] 5.3 `task lint` in `apps/web-ui`
- [x] 5.4 `openspec validate chat-sources-footer --strict`
- [ ] 5.5 Playwright spec run against a gateway built from this branch (skipped — no session-mode gateway from this worktree; the shared dev server runs stale code). Spec is discovered/compiled by Playwright and executed in CI where the branch gateway is up.

## 6. Ship

- [ ] 6.1 Commit on `feat/chat-sources-footer`, push, `gh pr create --base main` (do NOT merge)
