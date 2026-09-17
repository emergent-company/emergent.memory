# Tasks — refactor-agent-toolgroup-picker

Pure internal refactor of the agent tool-group picker markup. No behavior change; the existing `agent_ui_test.go` suite is the safety net and must stay byte-identical.

- [x] Extract `agentToolCountBadge(n int)` for the ghost XS wrench badge (was duplicated in `agentToolCapabilityGroup` and `agentToolGroup`). Verify: `templ generate` + `go test ./...` green.
- [x] Extract `agentPolicyOptions(value, inheritValue string)` for the Inherit/Allow/Ask/Deny option set (group select submits `"inherit"`, per-tool select submits `""`). Verify: `selectShowsValue` assertions unchanged.
- [x] Extract `agentToolOtherContainer(desc string, withTestID bool)` for the dashed "Other" container; `agentToolUncoveredGroup` and `agentToolOtherGroup` now share it. Verify: `data-testid="tool-group-other"` present only on the uncovered variant.
- [x] Make `agentToolOtherGroup` reuse `agentToolRowView` (removes the inline row + the now-dead `toolPolicySelect` wrapper). Verify: `value="ha_get_state" checked` still renders.
- [x] Extract `agentToolDisclosure` for the collapsible `<details>` shell; both `agentToolCapabilityGroup` and `agentToolGroup` now use it with slot helpers (`agentToolCapabilityLeading`/`agentToolCapabilityControls`/`agentToolGroupLeading`). Verify: `open data-testid="tool-group" data-tool-group="..."` ordering intact.
- [x] Convert the two sandbox availability dots to `components.StatusBadge(..., {Size: XS, Dot: true})`. Verify: `>available<` / `>unavailable<` render unchanged.
- [x] Keep the capability-group chevron class literal (`group-open/cap:rotate-180`) so Tailwind's source scan discovers it. Verify: `templ generate` + `task lint` green.
- [x] Restore the "Other" fallback row inset: give `agentToolRowView` an optional variadic row-class override and have `agentToolOtherGroup` pass the legacy inset-free class. Verify: `value="ha_get_state" checked` still renders, no extra horizontal inset.

## Verify

- `templ generate ./...`
- `go build ./...`
- `go test ./...` (1222 pass before and after)
- `task lint` (0 issues)
