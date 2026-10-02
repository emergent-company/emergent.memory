## 1. Catalog registry

- [ ] 1.1 Add `devgallery.go` with the catalog types (`GalleryEntry`, `GalleryLayer`, `GalleryDependency`) and a `componentGallery()` registry seeded with the `components` package and shared `package main` composites; verify `go build ./...` compiles
- [ ] 1.2 Add registry unit tests (`devgallery_test.go`): slugs are unique, every entry has a layer and source file, and known components (e.g. `PanelCard`, `StatusBadge`, `MetaGrid`) are present; verify `go test ./...`
- [ ] 1.3 Add curated fixtures in `devgallery_fixtures.go` for the `components` package and the frequently used go-daisy primitives; verify each fixture is referenced by a registry entry via a unit test
- [ ] 1.4 Add zero-value fallback rendering and a "needs fixture" flag for entries whose default render is empty; verify with a unit test using a deliberately empty component

## 2. Dependency graph generator

- [ ] 2.1 Add `cmd/componentgraph` that collects templ component functions returning `templ.Component` and walks their render call sites with `go/ast`; verify with a generator unit test over a small fixture directory
- [ ] 2.2 Emit `devgallery_graph_gen.go` (uses, used-by, go-daisy packages) with a do-not-edit header, wired via `//go:generate go run ./cmd/componentgraph`; verify `go generate ./...` then `go build ./...`
- [ ] 2.3 Assert determinism: running the generator twice on unchanged source produces identical output; verify in the generator test
- [ ] 2.4 Add a known-edge assertion (a shared composite → its go-daisy primitive) to `devgallery_test.go`; verify `go test ./...`
- [ ] 2.5 Add a staleness check that regenerates the graph and fails when the committed file differs; verify it passes on a regenerated tree

## 3. Preview rendering and isolation

- [ ] 3.1 Add `componentPreviewDoc` in `devgallery.templ`: a minimal standalone document linking the app `app.css` and the shell's htmx/Alpine assets, with the fixture component in a padded canvas; verify `templ generate` then `go build ./...`
- [ ] 3.2 Add the `/dev/components/preview/:slug` handler returning the preview document; verify via `httptest` that a known slug renders component markup and an unknown slug returns 404
- [ ] 3.3 Support fixture story variants via query parameters; verify with a unit test that a variant changes the rendered output

## 4. Gallery page and dependencies panel

- [ ] 4.1 Add `ComponentGalleryPage` in `devgallery.templ` listing entries grouped by layer/category with a selected-detail view; verify `templ generate` and `go build ./...`
- [ ] 4.2 Render the dependency panel (uses, used-by, external deps) and external-dependency detection (go-daisy packages, Alpine/Stimulus, htmx, `data-*`) from the fixture HTML; verify with a unit test asserting known dependencies for a sample component
- [ ] 4.3 Embed each preview as a lazy-loaded isolated iframe on the page; verify the rendered page contains a preview frame per listed slug via an `httptest` assertion

## 5. Routing and gating

- [ ] 5.1 Add the `MEMORY_COMPONENT_GALLERY` config flag (default on when `AuthMode != "session"`, off otherwise) in `config.go`; verify with config unit tests for both modes
- [ ] 5.2 Register `/dev/components` and `/dev/components/preview/:slug` in `main.go`, returning 404 when the flag is off; verify with `httptest` for both flag states
- [ ] 5.3 Render no sidebar navigation entry for the gallery; verify the rendered shell contains no gallery nav link

## 6. Verification

- [ ] 6.1 Run `templ generate ./...`, `go generate ./...`, `go build ./...`, and `go test ./...` from `apps/web-ui/gateway`; all succeed
- [ ] 6.2 Run `task lint` from `apps/web-ui/gateway`; clean
- [ ] 6.3 Run `task dev` and load `/dev/components` in the browser; verify the catalog renders, previews load, and an interactive component's overlay stays inside its preview
- [ ] 6.4 Add a Playwright e2e spec asserting the page loads and each listed slug's preview renders non-empty; verify it passes
- [ ] 6.5 Run `openspec validate web-ui-component-gallery --strict`; clean
