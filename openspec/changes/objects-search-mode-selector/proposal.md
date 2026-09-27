## Why

The Objects browser exposes its three search modes (full-text, hybrid, unified) as a segmented radio group next to a separate Search button. The segmented control gives no explanation of what each mode does, and it defaults to full-text — the weakest mode — so users get keyword-only results unless they know to change it. The control also reads as three independent buttons rather than one search action with a selectable mode.

## What Changes

- Replace the segmented mode radios + standalone Search button with a split button: a primary **Search** action that runs the currently selected mode, and an adjacent chevron that opens a dropdown of modes.
- Each dropdown option shows a label and a short second-line description of what the mode does.
- Default mode becomes **Unified** (fuses graph and text ranking). Previously an absent `mode` fell back to `fulltext`.
- The primary button's label reflects the active mode (e.g. "Unified Search").
- The mode is normalized to one of `unified | hybrid | fulltext` before rendering or dispatch (absent or unsupported → Unified), so the label, the value the form submits, and the search that runs always agree.
- Every mode option is a plain form submit button, so choosing a mode works with JavaScript disabled.

## Capabilities

### Modified Capabilities

- `object-browser`: the search control is a split button whose dropdown selects the search mode (Unified default, Hybrid, Full-text), each option documented with a short description; submitting runs the selected mode.

## Impact

- `apps/web-ui/gateway/objects.templ` — `objectsSearchForm` rewritten as a split dropdown button.
- `apps/web-ui/gateway/objects.go` — absent `mode` now defaults to `unified` (both the page handler and the partial handler).
- `apps/web-ui/gateway/objects_test.go` — render assertions updated for the new control.
- No server, API, schema, or DB change: `mode=fulltext|hybrid|unified` is unchanged; only the default when omitted changes.
