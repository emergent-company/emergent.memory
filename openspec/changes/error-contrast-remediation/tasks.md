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
- [ ] 4.5 Manual browser check: open the chat rail with `needs_input`, `failed`, `running`, and `done` rows and confirm each label reads at rest, on hover, and on the active row.

## 5. Scope B follow-up — theme error pair (needs human sign-off)

- [ ] 5.1 Design owner decides between keeping `--color-error-content: oklch(0.97 0.01 26)` (recommended change) and the rejected alternative of deepening `--color-error`, weighing ~18 destructive `btn-error` buttons + `#voice-call-btn.voice-calling`.
- [ ] 5.2 If approved: set `--color-error-content: oklch(0.2 0.04 26)` in the daisyUI theme block and re-measure all solid error surfaces (target ≥4.5:1) and error-as-text on `base-100` (must stay ≥4.5:1).
