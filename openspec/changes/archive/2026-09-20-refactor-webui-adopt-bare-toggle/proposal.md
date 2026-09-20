## Why

The gateway still hand-rolls six bare DaisyUI toggle checkboxes (`<input type="checkbox" class="toggle …">`) that emit only the input element — no wrapper `<div>`, no `<label>`, no field error — because each sits inside a caller-owned label/row/form that already supplies those. go-daisy now ships `form.ToggleInput` (go-daisy PR #19), a bare toggle component built for exactly this shape, with a locked, tested contract for its rendered attribute set and order.

Adopting it removes six duplicated, drift-prone input templates and single-sources the toggle markup in the library, mirroring the earlier dialog/disclosure/page-heading adoption lanes.

## What Changes

Bump the go-daisy pin to `781477d2006b4c56e6e2fa71c2b85f042c7bf3c1` and repoint the six remaining bare toggle sites onto `form.ToggleInput`:

1. `agent.templ` — the tool-group bulk-enable toggle (`groupEnabled.<id>`, `value="on"`, `aria-label`, `data-testid`, bulk `onchange`).
2. `agent.templ` — the per-tool row toggle (`name="tool"`, `value=<name>`, conditional `onchange`).
3. `mcp_servers.templ` — the server enable toggle inside the caller's own `<label>` (`aria-label`, `data-mcp-enabled-toggle`, `data-mcp-server`).
4. `mcp_servers.templ` — the per-tool toggle (`aria-label`, `data-mcp-tool-toggle`, `data-mcp-server`, `data-mcp-tool`).
5. `objects.templ` — the boolean property toggle paired with a sibling hidden `value="false"` input.
6. `schedules.templ` — the schedule enable toggle (`name="enabled"`, `aria-label`, form-submitting `onchange`).

Each site passes its existing class string through `Class`, keeps every `aria-label` / `data-*` / `onchange` in `Attrs`, and leaves surrounding markup (labels, forms, hidden inputs, wrapper divs) untouched.

`ToggleInput` renders a canonical attribute order — `type`, `name`, `value` (when set), `class`, `checked` (when true), then `Attrs` alphabetically. The hand-rolled sites wrote attributes in a different order (for example `name` before `type`, or `checked` before `class`), so the six inputs' rendered byte order changes while the element structure and attribute set stay semantically identical. Two sites (`mcp_servers.templ`) previously emitted no `name` attribute; `ToggleInput` always emits `name=""`, an empty attribute that carries no form value and is part of the component's locked contract.

## Capabilities

### Modified Capabilities

- `web-ui-components`: adds a requirement that bare toggle inputs render through go-daisy `form.ToggleInput`, with the canonical attribute order and the `name=""` normalization recorded explicitly.

## Impact

- `apps/web-ui/gateway/go.mod`, `go.sum` — go-daisy pin `062842a29737` → `781477d2006b`.
- `apps/web-ui/gateway/agent.templ`, `mcp_servers.templ`, `objects.templ`, `schedules.templ` — six hand-rolled bare toggle inputs repointed to `form.ToggleInput`.
- `apps/web-ui/gateway/agent_ui_test.go`, `mcp_relay_picker_test.go`, `objects_test.go` — assertions updated to the canonical attribute order (semantically identical markup).
- No route, handler, API, schema, or user-visible behavior change.
