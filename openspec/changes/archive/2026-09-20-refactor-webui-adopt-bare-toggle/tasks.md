# Tasks — refactor-webui-adopt-bare-toggle

Adopt go-daisy `form.ToggleInput` for the six remaining hand-rolled bare toggle inputs. Run commands from `apps/web-ui/gateway` unless stated otherwise. Generated `*_templ.go` are gitignored — run `templ generate`, never stage the output.

## 1 — Bump go-daisy pin

- [x] 1.1 `go get github.com/emergent-company/go-daisy@781477d2006b4c56e6e2fa71c2b85f042c7bf3c1` + `go mod tidy` + `GOWORK=off go mod vendor`; confirm `go.mod` pins `v0.12.1-0.20260920082201-781477d2006b`.

## 2 — Migrate the six bare toggle sites

- [x] 2.1 `agent.templ` group enable toggle → `form.ToggleInput` (Name/Value/Checked/Class + `aria-label`, `data-testid`, `onchange` in Attrs).
- [x] 2.2 `agent.templ` per-tool row toggle → `form.ToggleInput` (Name/Value/Checked/Class + conditional `onchange` in Attrs).
- [x] 2.3 `mcp_servers.templ` server enable toggle → `form.ToggleInput` (Checked/Class + `aria-label`, `data-mcp-enabled-toggle`, `data-mcp-server` in Attrs); caller `<label>` stays.
- [x] 2.4 `mcp_servers.templ` per-tool toggle → `form.ToggleInput` (Checked/Class + `aria-label`, `data-mcp-tool-toggle`, `data-mcp-server`, `data-mcp-tool` in Attrs).
- [x] 2.5 `objects.templ` boolean property toggle → `form.ToggleInput` (Name/Value/Checked/Class); sibling hidden `value="false"` input stays.
- [x] 2.6 `schedules.templ` schedule enable toggle → `form.ToggleInput` (Name/Checked/Class + `aria-label`, `onchange` in Attrs).

## 3 — Update order-only test assertions

- [x] 3.1 Update `agent_ui_test.go`, `mcp_relay_picker_test.go`, `objects_test.go` assertions that pinned the former hand-rolled attribute order (type/name/class/checked reordering only; attribute set and structure unchanged), each with a comment noting the pinned canonical order.

## 4 — OpenSpec

- [x] 4.1 Create `openspec/changes/refactor-webui-adopt-bare-toggle` (proposal, tasks, delta spec on `web-ui-components`). Verify: `openspec validate` clean.

## 5 — Verification

- [x] 5.1 `task css` && `templ generate ./...` && `go build ./...` && `go test ./...` && `task lint` — all clean.
