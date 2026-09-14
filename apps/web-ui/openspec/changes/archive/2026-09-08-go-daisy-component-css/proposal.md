## Why

The gateway loads a monolithic **473KB** pre-compiled go-daisy CSS bundle alongside its own 185KB `app.css`. Roughly 74% of the go-daisy bundle is daisyUI component styles for components Memory never renders, and ~25% is a 275-icon lucide safelist that can't be tree-shaken. That forces a large one-time download + CSS parse for every consumer, and go-daisy's bundled `nord` light default theme leaks after some htmx navigations (worked around today with unlayered `:root` overrides in `webui/css/app.css`).

go-daisy should ship **component-scoped CSS** and let each consumer compile only the components, utilities, and icons it actually uses.

## What Changes

- **go-daisy (upstream repo)**: extract its custom CSS from `assets/app.css` into package-grouped source files under `components/`; publish a per-package registry of daisyUI modules + CSS files; drop the fixed 275-icon safelist. go-daisy **keeps self-hosting** a slimmer `app.css` for its own gallery and other consumers.
- **Memory gateway (this repo)**: adopt consumer-side compilation:
  - `@source` the vendored go-daisy source so Tailwind generates only the utilities/icons the components actually use.
  - `@import` go-daisy's co-located CSS source files (sidebar/layout/alpine rules).
  - `@plugin "daisyui" { exclude: … }` pruning daisyUI modules the gateway never renders.
  - Drop the go-daisy 473KB `<link>` from `ui.templ`/`auth_ui.templ`.
  - Remove the theme-leakage workaround (`:root` unlayered overrides) once go-daisy's CSS is no longer loaded.
- Static, literal class strings in go-daisy components become a hard requirement (Tailwind scans source as plain text — computed class assembly is invisible).

## Capabilities

### New Capabilities

- `web-ui-css`: the gateway compiles and serves CSS for exactly the go-daisy components, utilities, and lucide icons it renders — no monolithic bundle, no unused-component or unused-icon payload, no theme leakage.

### Modified Capabilities

<!-- none -->

## Impact

- **go-daisy repo** (upstream, done directly there): custom CSS extraction to co-located package files, per-package registry, icon-safelist removal, slimmer self-hosted `app.css`.
- **gateway** (`this` repo): `ui.templ`/`auth_ui.templ` (drop `<link>`), `webui/css/app.css` (add `@plugin daisyui include` + `@source`), `Taskfile.yml`/`go.mod` (vendor go-daisy), `webui/static/css/app.css` regeneration.
- **Build pipeline**: the gateway's CSS build now depends on vendored go-daisy source; `go mod vendor` becomes a build prerequisite.
- **Risk**: any go-daisy component rendered but not declared in the `include` list (or whose utility/icon class isn't statically scannable) loses styling. Mitigated by a full UI regression pass across every page.
