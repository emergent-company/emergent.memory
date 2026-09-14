# 2026-09-09 — go-daisy htmx v4 migration + alfred vendor convergence

## Goal

Follow up on the htmx-extension analysis: go-daisy shipped a **half-finished htmx v2→4 migration** (bundled core `4.0.0-alpha8`, but ~20 component/gallery listeners, hx-on attrs, render helpers still on v2 camelCase event names that v4 never fires; sse/ws ext files were v2-era; `morph.js` had a legacy `htmx.defineExtension` tail that throws on v4; `render.SetMorph` emitted the v4-unknown `HX-Reswap: morph`). Complete the migration to **htmx 4.0.0 final**, then refresh alfred's vendored go-daisy to consume it.

## Outcome

Done (both repos, all pushed).

- **go-daisy**: migration finished on branch `feat/htmx-v4`, merged as PR #6 → master `bbfb8a3`; subsequent parallel-WIP commit `25bd7f1` also pushed.
- **alfred**: vendored go-daisy bumped to the merged commit `bbfb8a3` (`61eac68`), one stale v2 listener fixed (`15ce6e9` alongside parallel API-key `PasswordField` work pushed under user direction).

## Decisions

- **Rename to v4 event names; do not ship `htmx-2-compat`** — the repo was already half on v4 names; the compat shim leaves dead listeners (doesn't restore `response:error`/timeout), adds global side effects, and complicates `config.extensions` allow-lists. User confirmed.
- **Upgrade bundled core to 4.0.0 final + matching v4 ext files** — alpha8 predates the RC1 event/final naming and drifts from final docs. User confirmed.
- **Work in `/root/go-daisy` after syncing to `origin/master`** — checkout was 9 commits behind (parallel taglist/css work upstream) with another session's dirty WIP; user chose sync-first. Parallel WIP preserved in `git stash@{0}`, local-only commit guarded as `backup/9ccb99d-css`, restored/committed later as `25bd7f1`.
- **Keep `morph.js` but purge its v2 `defineExtension` tail** — htmx morphing is core in v4 (`innerMorph`/`outerMorph`, `HX-Reswap: innerMorph`), but the idiomorph global is kept for any Alpine-morph consumers; deleting the file would 404 external consumers. Morph bool fields/`MorphTag()` left as backward-compat no-ops.
- **v4 event-detail fixes over "keep body identical"** — v4 dispatches `{ctx}` detail (no top-level `elt`/`successful`), verified in the bundled core; csrf/modal/shell listeners updated to `ctx` shape. `theme-switcher`'s `historyRestore` listener removed: v4 restores are server refetches (normal swap → `after:settle` covers it); `after:history:update` fires only on push/replace, so mapping there would be semantically inverted.
- **alfred keeps its self-hosted htmx for now** — it deliberately self-hosts `4.0.0-beta6` + manual v2-compat config because go-daisy once bundled alpha8; converging onto go-daisy's bundled 4.0.0 final is deferred (task `adopt-godaisy-htmx-400`).

## Changes

### go-daisy (`/root/go-daisy`, master `bbfb8a3` via PR #6, then `25bd7f1`)

- `package.json`/`package-lock.json` — removed v2 `htmx-ext-sse`/`htmx-ext-ws`; added exact-pin `htmx.org@4.0.0` (npm `latest` is still 2.x).
- `staticfs/static/js/htmx.js` — alpha8 → 4.0.0 final dist.
- `staticfs/static/js/hx-sse.js`, `hx-ws.js` — added (v4 ext, auto-register); `htmx-sse.js`/`htmx-ws.js` deleted; `hx-head.js` refreshed to 4.0.0.
- `staticfs/static/js/morph.js` — purged `htmx.defineExtension("morph",…)` tail; idiomorph global only.
- Event renames to v4 (`afterSettle`→`after:settle`, `configRequest`→`config:request`, `afterSwap`→`after:swap`, `beforeHistorySave`→`before:history:update`, etc.) + `hx-on::after:request` colon-form attrs across `components/layout`, `components/ui/{progress-bar,theme-switcher,layout-customizer,csrf}`, `components/modal`, `components/table/datagrid`, `components/head`, `galleryruntime/{pages_shell,pages_detail}`, `cmd/nexus/internal/shell`.
- `components/ui/csrf.templ`, `components/modal/modal.templ`, `cmd/nexus/internal/shell/shell.templ` — listener bodies fixed to v4 `{ctx}` detail shape (were dead since alpha8).
- `render/render.go` — `SetMorph` now `HX-Reswap: innerMorph`; `OnAfterRequest/AfterSettle/ResponseError/BeforeRequest` emit `hx-on::after:request` etc.
- `stream/stream.go`, `layout.templ`, `head/dependencies.templ` — script paths → `/js/hx-sse.js`, `/js/hx-ws.js`.
- Docs: AGENTS.md / README.md / ROADMAP.md — bundled-JS lists, render-helper table, SSE/WS examples rewritten to v4.
- `tests/e2e/specs/*` — fixed stale `#gallery-shell` selectors (`#gallery-shell` was removed in an earlier shell refactor; suite had been red pre-migration). Detail pages now assert `#gallery-content`, index asserts the `h1`.
- `25bd7f1` (restored parallel WIP): combobox `labels` map (`ComboboxState(selected, labels)`), `x-data` attribute-spread, Alpine init `let root=$data`, + `lifecycle_test.go`.

### alfred (`/root/alfred`, master)

- `gateway/go.mod`/`go.sum` — go-daisy `v0.9.1-0.20260908113349-868b5d4b8de8` → `v0.9.1-0.20260909083930-bbfb8a3e3579` (merged migration). `vendor/` regenerated locally but is **gitignored** (build artifact) — not committed.
- `gateway/project_settings.templ` — provider test-connection listener `htmx:afterRequest`+`evt.detail.elt` → `htmx:after:request` + `evt.target === testBtn` (v4 dispatches on the requesting element; detail is `{ctx}`). This listener was dead under alfred's self-hosted beta6.
- `15ce6e9` — parallel session's provider API-key `PasswordField` refactor + css, committed/pushed at user request (not authored this session).

## Verification

- go-daisy: `task build:ui`, `go build ./...`, `go test ./...` — all pass. golangci-lint — 35 pre-existing issues only (unrelated files).
- go-daisy grep audit — zero v2 `htmx:camelCase` tokens in src/docs/generated; zero `defineExtension`/`htmx.` in `morph.js`.
- go-daisy gallery smoke — `/gallery/button` 200, served `htmx.js` = `4.0.0`, hx-sse/hx-ws 200, deleted v2 files 404.
- go-daisy Playwright e2e — after fixing stale selectors + installing the pinned chromium rev (playwright npm rev vs cached browser rev drift): **12 passed**.
- alfred: `go build ./...`, `go test ./...`, golangci-lint — all pass.
- **Templ mtime trap** (hit twice): `templ generate` reports `updates=0` and silently skips a file whose `.templ` and `*_templ.go` mtimes land in the same second. Force with `touch <file>.templ` then regenerate; verify with `rg`/md5. Both repos' `*_templ.go` regen in alfred is gitignored (generated at build), committed in go-daisy.

## Open questions / follow-ups

- alfred still self-hosts htmx `4.0.0-beta6` + manual v2-compat config in `ui.templ` while go-daisy vendors `4.0.0` final → converge (task `adopt-godaisy-htmx-400`).
- go-daisy `master` has since advanced `bbfb8a3`→`25bd7f1` (combobox labels/$data); alfred's vendor pin stays at `bbfb8a3` — harmless until alfred needs the new combobox behavior; fold into next go-daisy bump.
- go-daisy has no CI checks; e2e suite needed a browser-rev install + stale-selector fix to run (task `godaisy-e2e-ci`).
- Parallel-session WIP that was stashed, restored, and committed as `25bd7f1` — confirm owner is satisfied; `backup/9ccb99d-css` guard ref in go-daisy can be deleted.
- alfred `gateway/webui/static/css/app.css` dirty file from the parallel session was committed (`15ce6e9`); verify that was the intended css delta.

## Tasks

- [adopt-godaisy-htmx-400](../tasks/adopt-godaisy-htmx-400.md) — converge alfred onto go-daisy-bundled htmx 4.0.0 final, drop self-hosted beta6 + compat overlay
- [godaisy-e2e-ci](../tasks/godaisy-e2e-ci.md) — add go-daisy Playwright e2e to CI (none configured today)
