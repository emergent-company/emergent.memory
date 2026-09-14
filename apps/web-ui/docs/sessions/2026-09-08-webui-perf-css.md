# 2026-09-08 — Web UI perf, hx-boost nav, go-daisy CSS consolidation

## Goal

The user reported page transitions "not smooth" and asked to use devtools to check
whether transitions load a lot and what could be optimized. That grew into a full
session across three asks: (1) diagnose + optimize page-transition cost, (2) profile
and fix the slow blueprints page, (3) design + implement go-daisy's CSS distribution so
consumers ship only what they render.

## Outcome

Done. Eight gateway commits + one go-daisy publish + one OpenSpec change archived.

- Full-page reloads: **~520KB / ~650ms → ~175KB** (gzip) with go-daisy's 473KB CSS no
  longer re-downloaded per navigation (immutable cache on content-hashed URLs).
- List-page TTFB cut ~2–4× by parallelizing independent memory-backend calls; the MCP
  `initialize` round-trip is cached, not repeated per tool call.
- In-content links now partial-swap (hx-boost) instead of full reloads; sidebar nav was
  already partial.
- go-daisy CSS consolidated to a single consumer-compiled sheet: **658KB (two sheets)
  → 188KB raw / 30KB gzipped**, no theme leakage, no monolithic bundle served.

## Decisions

- **Add gzip middleware (skip `text/event-stream`)** — live chat SSE stays unbuffered; echo's
  Gzip compresses by length not content-type, so SSE had to be excluded explicitly.
- **Serve `/static/*` immutable** — go-daisy static URLs are already content-hashed
  (`?v=staticfs.Hash()`), so caching is safe; this removed the per-nav 473KB re-download.
- **Parallelize independent backend calls with `errgroup`** in list handlers (uiAgents,
  uiBlueprints, uiObjects, uiSkills, uiUsage) — one round-trip floor instead of N stacked.
  `MemoryClient` is stateless/read-only, so concurrent calls are safe.
- **Cache the MCP session id, keyed by token+project+org** — the service binds a session
  to that scope; re-initializing on every `schema-list`/`entity-query` cost a redundant
  round-trip. Stale session → empty result → clear + retry once.
- **hx-boost on `#main-content`** (`hx-target`/`hx-swap`/`hx-push-url`) — htmx v4 boost with
  a non-body target sends `HX-Request-Type: partial`, so the server's existing partial
  rendering kicks in with no shell re-render.
- **Convert `onsubmit="return confirm(...)"` → `hx-confirm`** — htmx v4 boost ignores inline
  onsubmit `return false` (submits even on cancel), verified empirically.
- **Exempt `<dialog>` modals from boost** (`hx-boost="false"` on `modalShell`) — boost
  broke native `method="dialog"`/`formmethod="dialog"` close (Cancel/backdrop).
- **go-daisy ships CSS *source*, keeps self-hosting** (re-scoped change) — removing its
  compiled `app.css` (task 2.2 as first written) self-breaks the library
  (`head/dependencies.templ` + gallery load it). Extract custom CSS to a co-located file,
  publish a registry, drop the 275-icon safelist, keep a slimmer self-hosted build.
- **`go mod vendor` gitignored + wired into `task css`** (not committed) — a committed 50MB
  `vendor/` is disproportionate for the shared repo, and the css build already depends on a
  dev symlink to `/root/go-daisy/node_modules` anyway. A blank import of
  `components/css` forces the dir (and `custom.css`) into `vendor/` for `@import`.
- **daisyUI `exclude`** (15 audited-unused modules) rather than `include` whitelist —
  whitelist mode also drops daisyUI base modules; the gateway renders ~48 of ~70 modules.

## Changes

### Gateway (alfred), my commits

- `646f6be` — `gateway/main.go`: gzip middleware (SSE skipped) + `cacheStatic` wrapper
  serving `/static/*` immutable.
- `a7389b3` — `gateway/ui.go`, `objects.go`, `usage.go`: errgroup fan-out in uiAgents /
  uiBlueprints / uiObjects / uiSkills / uiUsage; `go.mod` (x/sync direct).
- `7a99ddf` — `gateway/memory.go`: MCP session-id cache (`mcpSessionID`/`mcpClearSession`/
  key = token+project+org) + stale-session retry; `ListMemories` now shares `mcpToolCall`;
  `memory_test.go` (+2 tests).
- `ab76ea9` — `gateway/ui.templ`: hx-boost attributes on `<main id="main-content">`.
  `api_tokens.templ`, `org_context.templ`: onsubmit-confirm → hx-confirm.
- `af6e1b3` — `gateway/ui.templ`: `hx-boost="false"` on `modalShell` `<dialog>`.
  `refactor_exact_test.go`: golden HTML updated.
- `18b6ee6` — go-daisy dep → `868b5d4`; `webui/css/app.css` (`@source` vendored go-daisy,
  `@import` its custom.css, daisyUI `exclude`, removed `:root` theme-override);
  `ui.templ`+`auth_ui.templ` (dropped the `/static/css/app.css` link + `staticfs` import);
  `godaisy_css.go` (blank import); `Taskfile.yml` css task vendors first; `.gitignore`
  (vendor/); regenerated `webui/static/css/app.css`; `css_consolidation_test.go` (+3 tests).
- `e773b9f` — archive the `go-daisy-component-css` change; sync `web-ui-css` main spec.

### go-daisy repo

- `9ccb99d` (local) → published `868b5d4`: `assets/app.css` custom CSS moved to
  `components/css/custom.css` (app.css now `@import`s it; compiled output verified
  byte-equivalent), `components/css/registry.go` + test, 275-icon lucide safelist removed.

### OpenSpec

- `openspec/changes/go-daisy-component-css/` (created, re-scoped, applied, archived →
  `openspec/changes/archive/2026-09-08-go-daisy-component-css/`).
- `openspec/specs/web-ui-css/spec.md` (new main spec synced from the delta).

## Verification

- `go build ./...` — OK throughout (after the parallel session's broken-HEAD interlude
  resolved; their `c3b15d7` had undefined `orgSettingsPageData`/`OrgToolSettingsPage`).
- `go vet ./...` + `golangci-lint run ./...` — 0 issues.
- `go test ./...` — pass (incl. new MCP-cache + CSS-consolidation tests).
- DevTools traces — full reload 520KB→175KB; HTMX swap TTFB objects 218→~65ms,
  usage 215→~60ms; CSS byte-diff proved the go-daisy custom.css extraction faithful
  (identical rule sets, 321 icons preserved).
- Browser (devtools DOM, no screenshots — model can't read images): single stylesheet
  served, dark theme + `--color-base-*` correct, sidebar 256px, btn/table/menu computed
  styles correct, 44–48 mask-rendered icons/page, zero console errors across agents /
  objects / sessions / schema / tokens / usage / chat.

## Open questions / follow-ups

- `document.title` goes stale after hx-boost partial swaps (partial responses carry no
  `<title>`) — from the hx-boost change, separate from CSS. (→ task)
- Multipart file upload under hx-boost is source-verified (FormData) but not browser-tested
  (the devtools browser is on a different machine; can't pass a local file). (→ task)
- go-daisy gallery not visually re-verified after the safelist removal — the compiled-CSS
  oracle diff (identical rule sets) was used instead; a `task gallery` curl smoke hung on
  process management and was abandoned.
- Blueprints page still ~200ms TTFB gated by one slow memory-backend MCP `schema-list`
  call (server-side, not gateway) — profiling it is a memory-backend task.
- `vendor/` is gitignored; if CI/another machine builds `task css` without a prior
  `go mod vendor`, the `@source` path won't exist (the css task now runs vendor itself).
- go-daisy `components/css/registry.go` is static-analysis-derived; the optional Phase-3
  codegen (walk the consumer Go import graph to auto-derive the module set) is unbuilt.
- Parallel session WIP left untouched: `gateway/org_members_ui_templ.go` and a regenerated
  `webui/static/css/app.css` were dirty at session end (theirs, not committed by me).

## Tasks

- [hx-boost-title-sync](../tasks/hx-boost-title-sync.md) — fix stale document.title on partial swaps
- [verify-hx-boost-multipart-upload](../tasks/verify-hx-boost-multipart-upload.md) — browser-check file upload under hx-boost
- [godaisy-codegen-css-tool](../tasks/godaisy-codegen-css-tool.md) — derive daisyUI module set from Go imports
