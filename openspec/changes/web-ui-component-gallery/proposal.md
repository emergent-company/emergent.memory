## Why

The gateway has no single place to see its reusable templ components. `web-ui-component-conventions` already requires visual and interaction verification to happen "through the component gallery", but no gallery exists: reviewing a component's appearance, its layer, or what it depends on means browsing hundreds of templ definitions spread across `package main`, `components/`, and the vendored go-daisy library. This change builds that gallery as a dev-gated page so components can be reviewed and fixed visually in one place.

## What Changes

- Add a dev-gated **component gallery page** at `/dev/components`, rendered inside the existing app shell, listing the gateway's reusable templ components: the `components/` package, shared composites written inline in `package main`, and the go-daisy components the app renders. A `package main` component is in scope only when it is reused across two or more page features or encapsulates an invariant; the seed list is explicit and test-pinned (there is no reliable source-level "shared vs page-local" marker).
- Show, per component: layer (L0 go-daisy → L2 app composite), source file, props type, description, and a **dependency section** — which components it uses, which components use it, and its external dependencies (go-daisy package, Alpine/Stimulus controllers, htmx/`data-*` wiring).
- Add **fixture-backed previews**: each component renders with curated fake sample data inside an isolated iframe served from `/dev/components/preview/:slug`, so interactive components (dialogs, dropdowns, toasts, command palette) cannot escape into the shell and ids cannot collide. Components without a curated fixture fall back to zero-value props and are flagged as "needs fixture" when they render empty.
- Add a **static dependency-graph generator** (`cmd/componentgraph`) run via `go:generate`, emitting a compiler-checked graph file consumed by the registry.
- Gate the gallery behind a config flag (`MEMORY_COMPONENT_GALLERY`): enabled by default in dev (`AuthMode != "session"`), disabled by default in session/prod, returning 404 when disabled. The e2e/dev harness exports the flag so the Playwright spec can exercise the page. No production user-facing behaviour changes; no server, API, or schema change.

## Capabilities

### New Capabilities

- `web-ui-component-gallery`: a dev-gated catalog of the gateway's reusable templ components with layer/source metadata, a static uses/used-by dependency graph, external-dependency detection, and fixture-backed isolated previews.

### Modified Capabilities

(none)

## Impact

- `apps/web-ui/gateway/devgallery.go` (new) — catalog types, registry, HTTP handlers, external-dependency scan.
- `apps/web-ui/gateway/devgallery.templ` (new) — gallery page and the isolated preview document.
- `apps/web-ui/gateway/devgallery_fixtures.go` (new) — curated per-component fixtures.
- `apps/web-ui/gateway/devgallery_graph_gen.go` (new, generated) — the dependency graph.
- `apps/web-ui/gateway/cmd/componentgraph/` (new) — the generator.
- `apps/web-ui/gateway/main.go` — two dev-gated routes.
- `apps/web-ui/gateway/config.go` — `MEMORY_COMPONENT_GALLERY` flag.
- Tests: `devgallery_test.go`, `cmd/componentgraph/main_test.go`, and a Playwright e2e spec.
- No changes to the server, CLI, iOS app, database, or any public API.
