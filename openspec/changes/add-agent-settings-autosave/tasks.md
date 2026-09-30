## 1. General auto-save

- [x] 1.1 `webui/static/js/agent-settings.js`: debounced auto-save for a form marked `data-agent-autosave` (text inputs ~700 ms, selects/pickers/listbox promptly), unchanged-value skip, saving/saved/error status, retry, flush on in-app navigation, cancel on unload, implicit-submit interception.
- [x] 1.2 `agent.templ`: mark the General form `data-agent-autosave` + `data-autosave-url`; add the `agentAutosaveStatus` node and drop the shared save footer on this page only.
- [x] 1.3 `agent.go` + `main.go`: `POST /agents/:id/settings/general/autosave` returns JSON (200/422/502) via the existing section applier; PRG route unchanged.
- [x] 1.4 `ui.templ`: load `agent-settings.js`.
- [x] 1.5 Tests: Go autosave handler + General-render attrs; js-dom debounce/skip/error spec; live e2e General-edit spec switched to auto-save.

## 2. Tools inheritance + group disclosure

- [x] 2.1 `agent-settings.js`: recompute every policy select's `Inherit (<value>)` from the current default/group selections (explicit tool → group → default); cancel the `<summary>` activation for a group policy select click.
- [x] 2.2 `agent.templ`: mark the default select `data-tool-default-policy` and the Inherit option `data-inherit-option`.
- [x] 2.3 Tests: js-dom inheritance recompute + summary-activation-cancel spec; Go render asserts the new markers.

## 3. Verify

- [x] 3.1 `templ generate`; `go build ./...`; `go test ./...` from `apps/web-ui/gateway`.
- [x] 3.2 `node --check webui/static/js/agent-settings.js`.
- [x] 3.3 `npx playwright test --config=js-dom.config.ts` (the CI JS gate).
- [x] 3.4 `openspec validate add-agent-settings-autosave`.
