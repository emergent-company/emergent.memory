## 1. Muted scale — definition and migration

- [x] 1.1 Define the five `--color-muted-*` steps in `webui/css/app.css` (`@theme`, derived from `--color-base-content`), documented with the canonical-value table.
- [x] 1.2 Migrate gateway-owned `text-base-content/N` (`.templ`/`.go`/`.js`) to `text-muted-*` / `text-base-content`, mechanically and exhaustively (519 sites).
- [x] 1.3 Migrate the `app.css` foreground `color: color-mix(in oklab/oklch, var(--color-base-content) N%, transparent)` rules to `var(--color-muted-*)` / `var(--color-base-content)`.
- [x] 1.4 Update the rail-active selector in `app.css` (`.text-base-content/50|40|25` → `.text-muted|muted-faint|muted-subtle`) and the JS class toggles.
- [x] 1.5 Migrate the four standalone muted `opacity-N` icons/text; leave hover/reveal/ghost opacities.
- [x] 1.6 Update gateway-owned test goldens (`refactor_exact_test.go`, `components_test.go`, et al.); keep go-daisy-pinned goldens (`ui.Eyebrow` `/45`, `nav.PageHeading` `/55`, `form.FormControl` `/60`, empty-state `/20` `/50`) unchanged.

## 2. OpenSpec delta

- [x] 2.1 Add the "Muted text and icon foreground comes from a scale" requirement to `web-ui-css`.

## 3. Verification

- [x] 3.1 `templ generate` (from `apps/web-ui/gateway`) clean; `templ generate -check` passes.
- [x] 3.2 `go build ./...` and `go test ./...` pass from `apps/web-ui/gateway`.
- [x] 3.3 `task css` succeeds; compiled CSS contains `--color-muted-*` tokens and `text-muted-*` utilities, with zero `text-base-content/N` remnants from gateway sources.
- [x] 3.4 `task lint` passes (webui-templ, webui-go-build, webui-go-vet, webui-golangci-lint, webui-go-test, secrets all green).
