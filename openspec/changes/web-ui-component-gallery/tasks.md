## 1. Catalog registry

- [ ] 1.1 Add `devgallery.go` with the catalog types (`GalleryEntry` including `Render func() templ.Component` and the `AdditionalUses []string` escape hatch for dynamic renders, `GalleryLayer`, `GalleryDependency`) and an explicit `componentGallery()` seed covering the `components` package plus the in-scope `package main` composites (reused across ≥2 page features or encapsulating an invariant), each with name, layer, source file, props type, description, and a render closure; verify `go build ./...` compiles
- [ ] 1.2 Add registry unit tests (`devgallery_test.go`): slugs are unique, every entry has a layer, source file, props type, and description, known components (e.g. `PanelCard`, `StatusBadge`, `MetaGrid`) are present, and single-page exported helpers (e.g. `BoardColumns`) are absent; verify `go test ./...`
- [ ] 1.3 Add curated fixtures in `devgallery_fixtures.go` for the `components` package and the frequently used go-daisy primitives; fixtures SHALL be inert (`href="#"`, `type="button"` buttons, no live endpoint ids or real mutation wiring); verify each fixture is referenced by a registry entry and no fixture contains a live form action via a unit test
- [ ] 1.4 Add authored default render closures (zero values) wrapped in `recover()`, plus a "needs fixture" flag for entries whose default render is empty or panics; verify with unit tests for both a deliberately empty component and a nil-slot-panic component (e.g. `ListRow` with a nil leading component)
- [ ] 1.5 Add a render-without-panic smoke test iterating every registry entry, rendering its fixture and default closure under `recover()`, and asserting the "needs fixture" flag matches the result; verify `go test ./...`

## 2. Dependency graph generator

- [ ] 2.1 Add `cmd/componentgraph` that collects component definitions from both generated `*_templ.go` and hand-written `.go` in the gateway packages, selecting only functions whose exact return type is `templ.Component` (plus `templ.ComponentFunc` literals) and excluding `*_test.go`; walks render call sites with `go/ast`, unwrapping the emitted shapes — the `X(...).Render(...)` receiver call including the `templ.WithChildren` first-argument form, treating component-typed-parameter `.Render` as dynamic; resolves import aliases from each file's import block, covering unaliased `nav`/`table`/`layout`, the `ui`/`components` aliases, and the same package under two aliases (`fm`/`form`), and resolves same-package unqualified calls, recording an unresolved same-package callee as a package-granularity leaf (never a dangling edge); scans pages/helpers too so used-by is populated; verify with generator unit tests covering the exact return-type filter (reject `templ.Attributes`/`templ.ComponentScript`), `*_test.go` exclusion, a hand-written factory, a same-package unqualified call, the dual-alias case, `.Render`/`templ.WithChildren`, a component-typed-parameter dynamic render, and leaf-ify
- [ ] 2.2 Emit `devgallery_graph_gen.go` (uses, used-by, per-component go-daisy references for L0 entries, and go-daisy packages for external deps) with a do-not-edit header, wired via `//go:generate go run ./cmd/componentgraph`; verify `go generate ./...` then `go build ./...`
- [ ] 2.3 Assert determinism: running the generator twice on unchanged source produces identical output; verify in the generator test
- [ ] 2.4 Add assertions to `devgallery_test.go` for a forward edge (a shared composite → its go-daisy primitive) and for used-by (a `components` package entry lists one or more `package main` page callers); verify `go test ./...`
- [ ] 2.5 Add a staleness check that regenerates the graph (after `templ generate`) and fails when the committed file differs; it MUST NOT skip when `vendor/` is absent because it does not read go-daisy source; verify it passes on a regenerated tree
- [ ] 2.6 Assert graph consumers tolerate cycles: add a test with a mutual (L3↔L3) render pair and verify used-by/dependency rendering terminates via a visited-set without topological sorting; verify `go test ./...

## 3. Preview rendering and isolation

- [ ] 3.1 Add `componentPreviewDoc` in `devgallery.templ`: a minimal standalone document reproducing the shell's assets from their real roots and order — `webui.AssetPath("/css/app.css")`, `webui.AssetPath("/js/htmx.min.js")`, inline htmx config, `webui.AssetPath("/js/hx-alpine-compat.js")`, deferred `/static/js/alpine.js`, `webui.AssetPath("/js/app.js")`, plus any controller bundle the previewed component needs — with the fixture component in a padded canvas; the iframe SHALL carry `sandbox` without `allow-forms` and without `allow-same-origin` (adding `allow-scripts` only where a component needs it, never together with `allow-same-origin`); verify `templ generate` then `go build ./...`
- [ ] 3.2 Add the `/dev/components/preview/:slug` handler returning the preview document; verify via `httptest` that a known slug renders component markup and an unknown slug returns 404
- [ ] 3.3 Support fixture story variants via query parameters; verify with a unit test that a variant changes the rendered output

## 4. Gallery page and dependencies panel

- [ ] 4.1 Add `ComponentGalleryPage` in `devgallery.templ` listing entries grouped by layer/category with a selected-detail view; verify `templ generate` and `go build ./...`
- [ ] 4.2 Render the dependency panel (uses, used-by, external deps): go-daisy packages from the generated graph, client wiring (Alpine `x-*`, Stimulus `data-controller`, htmx `hx-*`, custom `data-*`) from scanning the component's `.templ` source body; verify with a unit test asserting known dependencies for a sample component
- [ ] 4.3 Embed each preview as a lazy-loaded isolated iframe on the page; verify the rendered page contains a preview frame per listed slug via an `httptest` assertion

## 5. Routing and gating

- [ ] 5.1 Add the `MEMORY_COMPONENT_GALLERY` config flag (explicit, default off in every mode; no `AuthMode`-based default because `task dev` is session mode) in `config.go`; verify with config unit tests for set and unset
- [ ] 5.2 Register `/dev/components` and `/dev/components/preview/:slug` in `main.go` with an in-handler flag check (route always registered; handler returns 404 when `MEMORY_COMPONENT_GALLERY` is off) so both states are httptest-able; exempt `/dev/` from project scoping (`projectScopePath`) and from `canonicalHostRedirect`; verify with `httptest` for flag on/off and for no-active-project (no `/orgs` redirect)
- [ ] 5.3 Render no sidebar navigation entry for the gallery; verify the rendered shell contains no gallery nav link
- [ ] 5.4 Enable the flag in the dev environment (`apps/web-ui/.env.example` + local `.env`), forward it through `apps/web-ui/docker-compose.yml` (its `environment:` block lists vars explicitly), and set it in the e2e/dev harness (`tests/e2e/run-e2e.sh`); verify `task dev` and `docker compose up` both serve `/dev/components` and `run-e2e.sh` exports it
- [ ] 5.5 Wire the generated graph into the dev loop and CI: `task dev`/air runs `templ generate` then `go generate ./...` so the graph is fresh; `cmd/componentgraph` is covered by the gateway `./...` build/vet/lint; verify the staleness check passes on a clean tree and fails after a stale edit

## 6. Verification

- [ ] 6.1 Run `templ generate ./...`, `go generate ./...`, `go build ./...`, and `go test ./...` from `apps/web-ui/gateway`; all succeed
- [ ] 6.2 Run `task lint` from `apps/web-ui/gateway`; clean
- [ ] 6.3 Run `task dev` and load `/dev/components` in the browser; verify the catalog renders, previews load, and an interactive component's overlay stays inside its preview
- [ ] 6.4 Add a Playwright e2e spec asserting the page loads and each listed slug's preview renders non-empty; run it under the dev harness with `MEMORY_COMPONENT_GALLERY=on` (`run-e2e.sh`, `AUTH_MODE=dev`); if it must run in the session-mode project against an external gateway, that gateway's deployment sets the flag; verify it passes
- [ ] 6.5 Run `openspec validate web-ui-component-gallery --strict`; clean
