## Context

Reusable templ components live in two places: the shared `components` package (26 definitions) and shared composites written inline in `package main` (the gateway's `*_templ.go`). The app also renders hundreds of go-daisy primitives. There is no catalog, no dependency view, and no preview surface; `web-ui-component-conventions` requires visual verification "through the component gallery" but the gallery does not exist.

go-daisy ships a reusable gallery (`galleryruntime`) plus dev-mode boundary helpers (`devmode`). `galleryruntime` exposes only `Serve(...)`, which starts its own blocking Echo server, and its ~4,400-line fixture seed lives in an `internal` package that cannot be imported. Generated `*_templ.go` files render each component as an ordinary Go call (`ui.Button(...)`, `components.PanelCard(...)`), which makes static call-site analysis tractable. See proposal.md — Why.

## Goals / Non-Goals

**Goals**
- One dev-gated page, inside the app shell, listing reusable components with metadata and a dependency view.
- A deterministic, generated dependency graph derived from render call sites.
- Isolated, fixture-backed previews safe for interactive components.

**Non-Goals**
- Cataloging every templ function (pages, page-private helpers) — spec scope is reusable components only.
- Auto-generating fixtures; the long tail uses zero-value props and is flagged when empty.
- Replacing Playwright visual coverage; the gallery complements it.
- Enabling the gallery in production by default.

## Decisions

**Native page, not an embedded `galleryruntime` server.** `galleryruntime` cannot mount into the existing Echo router (its only entry point starts a second server) and its fixture seed is unimportable, so reuse would still require authoring a registry — while leaving the gallery outside the app shell, outside its auth, and without a dependency graph. We adopt its useful patterns (iframe isolation, story variants) in a native page. Alternatives: run the upstream gallery as a separate dev server (rejected: outside the app, no dependency graph, duplicate fixtures); fork `galleryruntime` into the repo (rejected: vendoring maintenance for no behavioural gain).

**Static `go/ast` graph over generated templ Go.** Each component render is a normal call in `*_templ.go`, so a two-pass scan (collect component functions returning `templ.Component`, then walk bodies for calls into the known component set) yields edges, reverse edges, and the go-daisy packages rendered. This needs no new dependency and is deterministic. Alternative: templ's `parser/v2` (works but adds parsing wiring for the same result); `devmode` runtime boundaries (would require wrapping every component and captures only what is rendered in a given fixture, not the full static graph). Limitation: components rendered dynamically or via `templ.Raw` are not captured; the registry supports a manual `AdditionalUses` escape hatch and a test pins known edges.

**Iframe isolation for previews.** Dialog, dropdown, toast, and command-palette components, plus duplicate ids across many previews, make inline rendering unsafe and visually misleading. The preview route returns a slim standalone document that links the app's compiled `app.css` and the same htmx/Alpine assets the shell uses, so components render under real styles and scripts without touching the page DOM. Alternative: render inline in the gallery page (rejected: overlay escape and id collisions).

**Curated fixtures plus zero-value fallback.** Authoring fixtures for every component is week-scale effort. Curated fixtures cover the `components` package and the frequently used go-daisy primitives; everything else renders with zero-value props and is flagged "needs fixture" when the output is empty. This keeps the catalog useful immediately and honest about coverage.

**Config gating, not role gating.** The gallery is a developer tool, not an org-admin concern. A `MEMORY_COMPONENT_GALLERY` flag defaults on only in dev (`AuthMode != "session"`) and off otherwise; disabled routes 404 and no navigation entry renders. Rollback is unsetting the flag.

**Generator lives at `cmd/componentgraph` in the gateway module.** `go:generate` writes `devgallery_graph_gen.go` into `package main`, so the graph and the registry are compiler-checked together and no generated file needs to be hand-edited.

## Risks / Trade-offs

- **Generator misses dynamic renders** → registry supports a manual `AdditionalUses` field; a unit test asserts known edges (e.g. a composite → `ui.Card`).
- **Fixtures drift as components change** → fixture functions are typed Go against the components, so prop changes fail the build; an e2e test asserts each listed slug's preview renders non-empty.
- **go-daisy upgrade changes the used set** → the graph is regenerated; the go-daisy entry list derives from edges, so it tracks automatically.
- **Many iframes on one page** → previews use `loading="lazy"` and detail rendering is on demand.
- **Generated files are stale in CI** → a check regenerates the graph and fails if it differs (covered by a task).

## Migration Plan

Additive; no data or API migration. Dev environments get the gallery automatically via the default-on-in-dev flag. A session/prod deployment enables it only by setting the flag. Rollback is removing the flag (routes 404, no other behaviour affected).

## Open Questions

None.
