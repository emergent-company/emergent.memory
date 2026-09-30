## Why

#1291 fixed the Tools settings group-policy select by cancelling the click's
default in the capture phase (`preventDefault`) whenever the select sat inside a
`<summary>`. That works for the summary activation, but it was medium-confidence:
cancelling the click could affect opening the native `<select>` popup in a real
browser, and that path could not be verified headless (residual-risk note on
#1274).

The hack exists only because the select was a descendant of the interactive
`<summary>` (the disclosure activation target). Remove the need for it by moving
the select out of the summary entirely.

## What Changes

- The capability-group approval-policy select (`groupPolicy.<id>`) moves from the
  disclosure `<summary>` trailing slot into the group **body**, above the
  member-tool rows. It is now a sibling of the interactive `<summary>`, never a
  descendant, so its clicks can never run the summary activation.
- The summary keeps only the enable switch (`groupEnabled.<id>`) plus the
  leading label/description/count; it still toggles the group.
- `agent-settings.js` drops the capture-phase `preventDefault` click listener
  and the stale comment that justified it. The inheritance recompute
  (`syncToolPolicyLabels`: explicit tool → group → default) and its bindings /
  after-swap re-run are unchanged.
- The js-dom spec proves the new shape instead of the old cancellation: the
  policy select is not inside a `<summary>`, its click is not default-prevented,
  and the summary trigger is what toggles the group.
- The e2e group-policy round-trip opens the group (if collapsed) before reaching
  the now-in-body select.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities

- `agent-dashboard-ui`: the capability-group policy select renders in the group
  body, not the disclosure header; changing it cannot fold the group because it
  is no longer inside the summary activation target.
- `web-ui-components`: the `ui.Disclosure` mapping for a capability group puts
  the enable switch in the trailing slot and the policy select in the body.

## Impact

- `apps/web-ui/gateway/agent.templ` — split `agentToolCapabilityControls` into
  `agentToolCapabilityEnable` (summary trailing) and `agentToolGroupPolicy`
  (body row).
- `apps/web-ui/gateway/webui/static/js/agent-settings.js` — remove the
  capture-phase `preventDefault` listener and its comment.
- `apps/web-ui/tests/e2e/specs/js/agent-settings-wiring.spec.ts` — new fixture
  shape + replacement assertion.
- `apps/web-ui/tests/e2e/specs/agents/agent-tool-groups-ui.spec.ts` — open the
  group before selecting its policy.
- `apps/web-ui/gateway/visual_delta_pinning_test.go` — new
  `TestAgentToolGroupPolicySelectOutsideSummary` pins the select outside the
  `<summary>` against the real template.
