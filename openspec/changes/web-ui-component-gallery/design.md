## Context

Reusable templ components live in two places: the shared `components` package (24 shipping templ components — 20 exported plus 4 private helpers, alongside the `DialogAutoOpen` attributes helper; 2 further `^templ` defs are test-only fixtures) and shared composites written inline in `package main` (89 exported templ funcs, 75 of them `*Page`). The app also renders hundreds of go-daisy primitives. There is no catalog, no dependency view, and no preview surface; `web-ui-component-conventions` requires visual verification "through the component gallery" but the gallery does not exist.

go-daisy ships a reusable gallery (`galleryruntime`) plus dev-mode boundary helpers (`devmode`). `galleryruntime`'s page components and registry types are exported, but it exposes no mountable Echo handler — its only entry point, `Serve(...)`, starts its own blocking server — and its ~11,000-line fixture seed (`cmd/gallery/internal/gallery`, `seed.go` 9,814 + `seed_additions.go` 1,140) is an `internal` package that cannot be imported. Generated `*_templ.go` files render each component as an ordinary Go call (`ui.Button(...)`, `components.PanelCard(...)`), which makes static call-site analysis tractable. See proposal.md — Why.

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

**Native page, not an embedded `galleryruntime` server.** `galleryruntime` cannot mount into the existing Echo router (its `register` is unexported and the only entry point starts a second server) and its fixture seed is unimportable, so reuse would still require authoring a registry — while leaving the gallery outside the app shell, outside its auth, and without a dependency graph. We adopt its useful patterns (iframe isolation, story variants) in a native page. Alternatives: run the upstream gallery as a separate dev server (rejected: outside the app, no dependency graph, duplicate fixtures); fork `galleryruntime` into the repo (rejected: vendoring maintenance for no behavioural gain).

**Scope is an explicit, test-pinned seed list.** "Shared composite" has no objective source-level signal: of the 89 exported `package main` templ funcs, 75 are `*Page`, and several of the remaining 14 are exported but used by only one page feature (e.g. `BoardColumns`, `ShareLinksHeader`). The registry therefore carries a hand-maintained seed, and a component earns a place only when reused across two or more page features or when it encapsulates an invariant — mirroring the existing "a component earns its place" convention. A unit test pins the seed and the exclusion of page/private helpers.

**Static `go/ast` graph over generated templ Go.** Each component render is a normal call in `*_templ.go`, so a two-pass scan (collect component functions returning `templ.Component`, then walk bodies for calls into the known component set) yields edges, reverse edges, the individual go-daisy component references (which become L0 catalog entries and `uses` targets), and the go-daisy packages rendered (for the external-dependency field). The walk MUST unwrap the shapes templ actually emits: the component call is the receiver of `.Render(...)` (e.g. `components.PanelCard(...).Render(templ.WithChildren(ctx, …), buf)`), so the scanner matches the `X` of a `X(...).Render(...)` `SelectorExpr`, including the `templ.WithChildren` first-argument form. Component-typed parameters render as `param.Render(ctx, buf)` and are inherently dynamic. This needs no new dependency and is deterministic. Alternative: templ's `parser/v2` (works but adds parsing wiring for the same result); `devmode` runtime boundaries (would require wrapping every component and captures only what is rendered in a given fixture, not the full static graph). Limitation: components rendered dynamically (component-typed params) or via `templ.Raw` are not captured; the registry supports a manual `AdditionalUses` escape hatch and a test pins known edges.

**Iframe isolation for previews.** Dialog, dropdown, toast, and command-palette components, plus duplicate ids across many previews, make inline rendering unsafe and visually misleading. The preview route returns a slim standalone document that links the app's compiled `app.css` and the shell's htmx/Alpine assets in the shell's required order — htmx, then the htmx/Alpine compatibility extension and inline htmx config, then deferred Alpine — so interactive components initialise correctly without touching the page DOM. Alternative: render inline in the gallery page (rejected: overlay escape and id collisions).

**Curated fixtures plus zero-value fallback.** Authoring fixtures for every component is week-scale effort. Curated fixtures cover the `components` package and the frequently used go-daisy primitives; everything else renders with zero-value props and is flagged "needs fixture" when the output is empty, where "empty" means no non-whitespace text and no element in the rendered output. This keeps the catalog useful immediately and honest about coverage.

**External dependencies come from two sources.** Both the per-component go-daisy references (L0 entries + `uses` edges) and the package-level go-daisy usage (external deps) come from the generated static graph; client wiring (Alpine `x-*`, Stimulus `data-controller`, htmx `hx-*`, and custom `data-*` markers) is detected by scanning the component's `.templ` source body, not the rendered fixture HTML — zero-value or partial renders do not emit every marker, so HTML-based detection would under-report.

**Config gating, not role gating.** The gallery is a developer tool, not an org-admin concern. A `MEMORY_COMPONENT_GALLERY` flag defaults on only in dev (`AuthMode != "session"`) and off otherwise; disabled routes 404 and no navigation entry renders. Rollback is unsetting the flag.

**Generator lives at `cmd/componentgraph` in the gateway module.** `go:generate` writes `devgallery_graph_gen.go` into `package main`, so the graph and the registry are compiler-checked together and no generated file needs to be hand-edited.

## Risks / Trade-offs

- **Generator misses dynamic renders** → the walk unwraps `X(...).Render(...)`/`templ.WithChildren` but component-typed params and `templ.Raw` remain uncaptured; the registry supports a manual `AdditionalUses` field and a unit test asserts known edges (e.g. a composite → `ui.Card`).
- **Fixtures drift as components change** → fixture functions are typed Go against the components, so prop changes fail the build; an e2e test asserts each listed slug's preview renders non-empty.
- **e2e suite runs in session mode, so the default-off gate hides the gallery** → the e2e/dev harness exports `MEMORY_COMPONENT_GALLERY=on` for the run that exercises the gallery spec.
- **go-daisy upgrade changes the used set** → the graph is regenerated; the go-daisy entry list derives from edges, so it tracks automatically.
- **Many iframes on one page** → previews use `loading="lazy"` and detail rendering is on demand.
- **Generated files are stale in CI** → a check regenerates the graph and fails if it differs (covered by a task).

## Migration Plan

Additive; no data or API migration. Dev environments get the gallery automatically via the default-on-in-dev flag. A session/prod deployment enables it only by setting the flag; the e2e/dev harness exports it for the gallery spec. Rollback is removing the flag (routes 404 after auth, no other behaviour affected).

## Open Questions

None.
