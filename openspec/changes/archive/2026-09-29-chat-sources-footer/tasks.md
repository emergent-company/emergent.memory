# Tasks

Worktree: `/root/emergent.memory-wt/chat-sources-footer` (branch `feat/chat-sources-footer`).

## 1. Gateway — type map

- [x] 1.1 `type_ui.go`: `objectTypeUI{Label,Color}` + `objectTypeMapFor(objectTypes, relationshipTypes []CompiledType) map[string]objectTypeUI` (skips shadowed/empty names); `objectTypeMapJSON` serializer (`encoding/json`, HTML-escaped so it cannot break out of a `<script>`); `objectTypeMapScript` (templ treats `<script>` bodies as raw text, so the element is emitted raw); `objectTypeUIMap` (project-keyed TTL cache + singleflight, best-effort)
- [x] 1.2 `ui.templ`: emit `<script type="application/json" id="memory-object-types">` in the shell
- [x] 1.3 `ui.go page()`: build the map best-effort from `GetCompiledTypes`; empty map on error
- [x] 1.4 Unit tests: map includes label/color, merges relationship types, skips empty/shadowed names; serializer round-trips and escapes `<`/`>`/`&`; shell renders the script for non-empty and empty maps

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

- [x] 4.1 Playwright spec `specs/sessions/chat-sources-footer.spec.ts` asserting collapsed-by-default footer, expand, and the `chat-source*` testids (deterministic, no LLM: builds a bubble in-page and calls `attachSources`). Seeds `<script id="memory-object-types">` with a map label distinct from the humanizer output (`LegalParagraph` → `Legal provision` vs `Legal paragraph`) and asserts the rendered badge equals the map label, plus a second badge asserting the humanizer fallback for a type absent from the map.

## 5. Verify

- [x] 5.1 `templ generate` (web-ui)
- [x] 5.2 `go build ./...` && `go test ./...` in `apps/web-ui/gateway`
- [x] 5.3 `task lint` in `apps/web-ui`
- [x] 5.4 `openspec validate chat-sources-footer --strict`
- [x] 5.5 Playwright spec run against a gateway built from this branch (3/3 passed). Recipe: `task build` in `apps/web-ui/gateway` → run `./memory` on `:18095` with the shared `memory-dev` env (`AUTH_MODE=session`, `SESSION_SECRET`, `ZITADEL_*`, `MEMORY_URL=https://api.dev.emergent-company.ai`; `PUBLIC_BASE_URL`/`ZITADEL_REDIRECT_URI` repointed to `:18095`); mint a session by running the e2e `setup` project against `:8095` and reuse the host-scoped cookie via a throwaway Playwright config in `/tmp/opencode`. Rendered `[data-testid=chat-source-type]` = `Legal provision` (map label) for `LegalParagraph`, and `Zzz unknown type` (humanizer fallback) for the absent type `ZZZUnknownType`.

## 6. Ship

- [x] 6.1 Commit on `feat/chat-sources-footer`, push, and update PR #1228 (do NOT merge)
