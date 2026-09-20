## ADDED Requirements

### Requirement: Bare toggle inputs render through go-daisy form.ToggleInput

The six bare DaisyUI toggle checkboxes (agent tool-group and per-tool rows, MCP server and per-tool toggles, object boolean property, schedule enable) SHALL render through go-daisy `form.ToggleInput`, and the gateway SHALL NOT hand-roll these inputs.

#### Scenario: Each site maps to ToggleInput props

- **WHEN** one of the six bare toggle sites is rendered
- **THEN** it calls `form.ToggleInput` with the site's existing `Class` string passed through `Class`, its `Name`/`Value`/`Checked` fields set, and every `aria-label`, `data-*` hook, and `onchange` handler carried in `Attrs`, with the caller's surrounding markup (labels, forms, hidden inputs, wrapper divs) left untouched

#### Scenario: Canonical attribute order is the contract

- **WHEN** a bare toggle input is rendered
- **THEN** it emits the input in `ToggleInput`'s canonical order — `type`, `name`, `value` (when set), `class`, `checked` (when true), then `Attrs` alphabetically — and the resulting markup is semantically identical to the former hand-rolled input, differing only in attribute byte order and class placement

#### Scenario: Empty name on the MCP toggles is recorded

- **WHEN** the MCP server enable toggle or the MCP per-tool toggle is rendered
- **THEN** it emits `name=""` because `ToggleInput` always emits the `name` attribute, where the former hand-rolled inputs had none; the empty attribute carries no form value and is part of the component's locked contract
