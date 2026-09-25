## Context

`gateway/agent.templ` renders the agent Settings → Tools picker. Five blocks of markup are duplicated across the capability-group, source-group, and "Other" fallback renderers. The markup is test-locked by `agent_ui_test.go`, which asserts on exact `data-testid`, `name`, `value="on"`/`checked`, and option `value` strings (e.g. `open data-testid="tool-group" data-tool-group="graph-write"`, `selectShowsValue(html, "groupPolicy.web", "inherit")`).

## Approach

Extract unexported templ helpers inside `agent.templ` (no changes to `components/`, which another lane owns):

- `agentToolDisclosure(groupName, detailsClass, summaryClass string, detailsAttrs, summaryAttrs templ.Attributes, open bool, leading, controls templ.Component)` — the `<details>`/`<summary>`/chevron shell. `groupName` drives the chevron's `group-open[/name]:rotate-180` selector; `detailsAttrs`/`summaryAttrs` carry the capability group's `data-testid`/`data-tool-group` markers (nil for source groups).
- `agentToolCountBadge`, `agentPolicyOptions`, `agentToolOtherContainer` — small value helpers.

Each extraction preserves exact rendered attribute order and value spaces. The group policy select keeps `value="inherit"` for Inherit; the per-tool select keeps `value=""`. The capability group keeps its `data-testid="tool-group"` / `data-tool-group` markers and `items-start` summary alignment; source groups keep `items-center` and nil attributes.

`agentToolRowView` keeps the padded default (`px-3` + hover background) for rows inside plain group bodies, but takes an optional variadic row-class override so the "Other" fallback container (which already supplies `p-3`) can pass the legacy inset-free class (`flex items-center gap-2.5 py-2`, no `px-3`, no hover background) and stay aligned with its "Other" eyebrow.

## Goals / Non-Goals

**Goals:** single-source each duplicated block; zero rendered-output change.

**Non-Goals:** any behavior, styling, or markup change; moving helpers into `components/` (another lane's package).
