## 1. Measure the rail badge label across row states

- [x] 1.1 Enumerate the real rail row backgrounds from `app.css`: `#chat-rail` is `bg-base-100`; `#chat-rail .list-row:hover` adds `oklch(1 0 0 / 0.04)`; `#chat-rail .list-row[data-active="true"]` adds `oklch(0.74 0.135 78 / 0.13)`.
- [x] 1.2 Compute OKLCH→sRGB→WCAG for each bucket label on its tint composited over each row state. Result: `needs_input` 7.07/6.30/5.56 pass, `running` 6.02/5.37/4.75 pass, `failed` 3.96/3.57/3.17 fail, `done` 3.65/3.46/3.25 fail.
- [x] 1.3 Compute the same for the dock captions: `.dock-count-label` (via `.dock-head`) 3.89 fail; `.dock-count` pill 9.91 pass.

## 2. Fix the failing rail badge labels (both copies)

- [x] 2.1 `failed` label: `--color-error` → `color-mix(in oklab, var(--color-error) 60%, var(--color-base-content))` in `webui/css/app.css`. Verify worst-case (active row) = 5.22:1.
- [x] 2.2 `done` label: `--color-base-content` 45% → 65% in `webui/css/app.css`. Verify worst-case (active row) = 5.03:1.
- [x] 2.3 Mirror 2.1 and 2.2 in the injected `<style>` string in `webui/static/js/chat-components.js`.
- [x] 2.4 Confirm geometry, bucket tint/border alphas, the `needs_input`/`running` labels, and the `.memory-rail-count` pairs are unchanged.

## 3. Fix the failing dock caption (both copies)

- [x] 3.1 `.dock-head` colour 45% → 65% `--color-base-content` in `webui/css/app.css`. Verify = 6.79:1 over the dock background.
- [x] 3.2 Mirror 3.1 in the injected `<style>` string in `webui/static/js/chat-components.js`.

## 4. Verification

- [x] 4.1 `task css` — regenerated `webui/static/css/app.css`; confirmed the new `failed`/`done`/`.dock-head` rules are present and no `currentColor` remains in `.memory-rail-count`.
- [x] 4.2 `templ generate`, `go build ./...` — OK.
- [x] 4.3 `go test ./...` — OK.
- [x] 4.4 `golangci-lint run ./...` — 0 issues.
- [x] 4.5 Manual browser check (built `webui/static/css/app.css`, real rail selectors, each bucket on base/hover/active rows): every label ≥4.5:1. Measured `needs_input` 7.07 / 6.31 / 5.50, `failed` 6.54 / 5.91 / 5.20, `running` 5.99 / 5.34 / 4.73, `done` 6.03 / 5.52 / 5.04, dock caption 7.09.

## Out of scope — tracked separately

The theme-level `--color-error` / `--color-error-content` pair (3.52:1, capping every solid error surface) is deliberately **not** implemented in this change: it repaints every destructive button in the app, so it is a brand-owner decision. Measurements, the recommended token value, and the rejected alternative are recorded in the proposal and tracked as issue #569.
