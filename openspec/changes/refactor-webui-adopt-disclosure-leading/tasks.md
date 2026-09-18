# Tasks — refactor-webui-adopt-disclosure-leading

Final adoption lane: migrate the last two non-catalog local components onto go-daisy. Run commands from `apps/web-ui/gateway` unless stated otherwise. Generated `*_templ.go` are gitignored — run `templ generate`, never stage the output.

## 1 — Bump go-daisy pin

- [x] 1.1 `go get github.com/emergent-company/go-daisy@f64017d5da6ce212f3d94c417a881a2a240c36e7` + `go mod tidy` + `GOWORK=off go mod vendor`; confirm `go.mod` pins `f64017d5da6c` and `vendor/.../components/ui/disclosure.templ` carries `DetailsBase`/`SummaryBase`/`BodyBase`/`SummaryAttrs` and `vendor/.../components/nav/page-heading.templ` carries `Leading`/`HideBreadcrumbs`. Verify: `go build ./...` green.

## 2 — Tool-group disclosure onto ui.Disclosure

- [x] 2.1 `agentToolCapabilityGroup` → `ui.Disclosure(ui.DisclosureProps{GroupClass: "group/cap", ItemsStart: true, Attrs: {data-testid, data-tool-group}, SummaryAttrs: {data-testid: "tool-group-header-*"}, Open})` with the body content as children (Disclosure's default `BodyBase` supplies the `gap-2 border-t p-3` wrapper).
- [x] 2.2 `agentToolGroup` → `ui.Disclosure(ui.DisclosureProps{DetailsBase: "rounded-box border "+BorderClass, BodyBase: "flex flex-col border-t "+BodyBorderClass, Open})` with the tool rows as children.
- [x] 2.3 Delete `agentToolDisclosure`. Verify: `agent_ui_test.go` (ToolGroups + FullMembership) and `refactor_exact_test.go` green.

## 3 — detailHeaderLeading onto nav.PageHeading.Leading

- [x] 3.1 `detailHeader` forwards `Leading` into `nav.PageHeading` (ignored when `Bare`); delete `detailHeaderLeading` and `detailHeaderTitleRow`.
- [x] 3.2 Update `pageHeader` comment (it stays local for the `mt-2` + eyebrow reasons; the breadcrumbs reason is obsolete). Verify: `refactor_exact_test.go` and `TestAgentDashboardHeaderLeadingTile` green.

## 4 — OpenSpec

- [x] 4.1 Create `openspec/changes/refactor-webui-adopt-disclosure-leading` (proposal, tasks, delta spec on `web-ui-components`). Verify: `openspec validate` clean.

## 5 — Verification

- [x] 5.1 `task css` && `templ generate ./...` && `go build ./...` && `go test ./...` && `task lint` — all clean.
