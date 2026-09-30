## 1. Move the policy select out of the summary

- [x] 1.1 In `agent.templ`, split `agentToolCapabilityControls` into `agentToolCapabilityEnable` (summary trailing: hidden `groupWasEnabled.<id>` + `groupEnabled.<id>` switch) and `agentToolGroupPolicy` (body row: `<label for>` + `groupPolicy.<id>` select). Preserve `name`, `data-testid="tool-group-policy-<id>"`, `aria-label`, and the `agentPolicyOptions` Inherit/Allow/Ask/Deny options.
- [x] 1.2 Render `agentToolGroupPolicy` in the disclosure body (above the member-tool rows) for every group except the display-only "other" group; keep `agentToolCapabilityEnable` as the trailing slot.

## 2. Remove the click-cancelling hack

- [x] 2.1 Delete the capture-phase `click` listener that `preventDefault()`s a click on `select[name^="groupPolicy."]` inside a `<summary>` in `agent-settings.js`.
- [x] 2.2 Update the file header comment (behaviour 2) to state the select lives in the body and no click is cancelled. Keep `syncToolPolicyLabels` and its `change` binding + `htmx:after:swap` boot.

## 3. Tests

- [x] 3.1 `agent-settings-wiring.spec.ts`: move the group policy select into the body in `TOOLS_FORM`; replace the "summary-activation is default-prevented" assertion with one proving the new structure — select is not inside a `<summary>`, its click is not default-prevented, clicking it leaves the group open, and the summary trigger toggles the group.
- [x] 3.2 `agent-tool-groups-ui.spec.ts`: open the capability group (if collapsed) before selecting its policy, since the select now lives in the body.
- [x] 3.3 `visual_delta_pinning_test.go`: `TestAgentToolGroupPolicySelectOutsideSummary` renders a real capability group and asserts the `groupPolicy.<id>` select is not a descendant of the `<summary>` while the `groupEnabled.<id>` switch is.

## 4. Verification

- [x] 4.1 `templ generate` + clean `git status --porcelain` (generated `*_templ.go` is gitignored).
- [x] 4.2 `go build ./...` + `go test ./...` in `apps/web-ui/gateway`.
- [x] 4.3 `node --check` on `agent-settings.js`.
- [x] 4.4 js-dom Playwright gate (`npx playwright test --config=js-dom.config.ts`) green.
- [x] 4.5 `git diff | grep -E '^[+-].*(id=|data-testid|hx-|name=)'` reviewed — only the intentional `id` addition; `name`/`data-*` hooks preserved.
