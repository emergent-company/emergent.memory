## Why

An agent's appearance (icon + colour) can be set on the General settings form, which
auto-saves — there is no Save button. The go-daisy `ColorPicker` commits a preset swatch
(or the Clear button) by setting its text field value *programmatically*, which dispatches
no `input`/`change`. The auto-save wiring only scheduled a save for the picker's native
`change`, so choosing a colour from a preset was silently discarded. The reported symptom:
"the colour assigned to the agent was not preserved/saved."

A second, related weakness: `applyAgentGeneralSection` rebuilt `uiConfig` from
`FormValue("icon")`/`FormValue("color")` on every save, so any General save whose body
omitted the Appearance pickers (`icon`/`color` keys absent) would wipe a stored appearance
by defaulting both to empty. The rendered form always submits both keys, but the endpoint
should be lossless for partial bodies regardless.

## Changes

- `agent-settings.js` (debounced auto-save): schedule a save on clicks of the colour
  picker's preset swatches (`[data-gd-color-preset]`) and clear button
  (`[data-gd-color-clear]`), matching the existing icon-picker option/reset handling.
- `agent.go` (`applyAgentGeneralSection`): only rewrite `UIConfig` when the request
  actually carries an `icon` or `color` field; an explicit empty value still clears.
- Regression coverage: a hermetic `js-dom` wiring spec (colour preset/clear scheduling),
  Go unit subtests (partial body preserves / explicit empty clears), and Playwright e2e
  (agent creation options + settings preset auto-save persistence). The stale
  `agent-icon-picker-ui` spec, which still waited for a removed Save button, is migrated
  to the auto-save path.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-appearance`: the Web UI edit requirement now states that appearance changes made
  through the auto-saving General form persist regardless of how the value was chosen
  (typing, preset swatch, native swatch, clear), and that a General save which does not
  carry the appearance fields preserves the stored appearance.

## Impact

- **Gateway files:** `webui/static/js/agent-settings.js`, `agent.go`, `agent_ui_test.go`,
  `tests/e2e/specs/js/agent-settings-wiring.spec.ts`,
  `tests/e2e/specs/agents/agent-icon-picker-ui.spec.ts`,
  `tests/e2e/specs/agents/agent-create-appearance-ui.spec.ts`.
- **No API, schema, or migration change.** `uiConfig` wire shape unchanged.
