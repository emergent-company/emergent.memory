## 1. Usage audit

- [x] 1.1 Enumerate the daisyUI component modules the gateway renders by scanning every `.templ`/`.go` file for component classes; produce the `include:` list and verify it covers every go-daisy component
- [x] 1.2 Enumerate the lucide icons used by the gateway and by go-daisy components; produce the icon set and verify no rendered icon is missing

## 2. go-daisy upstream (go-daisy repo, re-scoped: library keeps self-hosting)

- [x] 2.1 Extract go-daisy's custom CSS (sidebar/layout/topbar/view-transition/alpine/icon rules) out of `assets/app.css` into package-grouped co-located `.css` files under `components/` (e.g. `layout/layout.css`, `nav/nav.css`, `alpine/alpine.css`, `ui/ui.css`); verify `task build:ui` still compiles and the gallery is unchanged
- [x] 2.2 Publish a per-package registry mapping each go-daisy component package to the daisyUI modules and co-located CSS files it needs (Go file under `css/` or `components/`); verify it loads from a unit test
- [x] 2.3 Remove the fixed 275-icon lucide safelist from `assets/app.css` (icons become scannable via `@source` of go-daisy's own components); rebuild the self-hosted `staticfs/static/css/app.css` and verify go-daisy's gallery (`task gallery`) still renders icons correctly
- [x] 2.4 Commit + push go-daisy

## 3. Gateway consumer-side compilation

- [x] 3.1 Update `gateway/go.mod` to the new go-daisy commit and run `go mod vendor`; verify `vendor/github.com/emergent-company/go-daisy` exists and `go build ./...` succeeds
- [x] 3.2 Rewrite `gateway/webui/css/app.css`: add `@source` for the vendored go-daisy components (utilities/icons on demand), `@import` the go-daisy co-located CSS files, and add `@plugin "daisyui" { exclude: <unused modules> }` to prune daisyUI; verify `task css` compiles
- [x] 3.3 Remove the go-daisy `<link rel="stylesheet">` from `ui.templ` and `auth_ui.templ`; verify `templ generate` succeeds and page HTML omits `/static/css/app.css`
- [x] 3.4 Remove the unlayered `:root` theme-override block from `webui/css/app.css`; verify the brand dark theme still resolves
- [x] 3.5 Regenerate `gateway/webui/static/css/app.css`; verify the compiled size is below the previous go-daisy + gateway total (~658KB)

## 4. Verification

- [x] 4.1 Add a Go test asserting a rendered page's `<head>` does not reference `/static/css/app.css`; verify the test passes (`go test ./...`)
- [x] 4.2 Add a build assertion that the compiled CSS excludes a sentinel daisyUI module not needed (assert an unused module's class is absent); verify it passes
- [x] 4.3 Add a determinism check (compile CSS twice, assert byte-identical output); verify it passes
- [x] 4.4 Run a full UI regression pass in the browser across every page (agents, objects, documents, schema, blueprints, skills, backups, sessions, usage, chat, settings, org/profile) and confirm no unstyled components, no missing icons, and the brand dark theme persists through htmx swaps and full loads
