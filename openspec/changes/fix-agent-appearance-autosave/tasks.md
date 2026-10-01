## 1. Fix the auto-save wiring

- [x] 1.1 `agent-settings.js`: schedule the debounced save on `[data-gd-color-preset]` /
      `[data-gd-color-clear]` clicks (preset/clear set the text field programmatically).
- [x] 1.2 `agent.go`: make `applyAgentGeneralSection` lossless — rewrite `uiConfig` only
      when the request carries `icon`/`color`; an explicit empty value still clears.
- [x] 1.3 Go unit subtests: partial body preserves the stored appearance; explicit empty
      clears it.
- [x] 1.4 Hermetic js-dom wiring test: preset click and clear click each schedule a save
      carrying the picker value.
- [x] 1.5 Migrate `agent-icon-picker-ui.spec.ts` from the removed Save button to the
      auto-save path; add a preset-click persistence case.
- [x] 1.6 New `agent-create-appearance-ui.spec.ts` covering the create-modal appearance
      options (typed colour, icon + preset colour, neither).

## 2. Verification

- [x] 2.1 `go build ./...`, `go test ./...` (gateway).
- [x] 2.2 `npx playwright test --config=js-dom.config.ts`.
- [x] 2.3 Live Playwright mutations specs (create appearance + icon picker).
- [x] 2.4 `task lint` (gateway / web-ui — green) (gateway / web-ui).
