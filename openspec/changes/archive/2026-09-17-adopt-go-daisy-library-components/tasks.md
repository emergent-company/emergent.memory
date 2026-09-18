# Tasks — adopt-go-daisy-library-components

Pure markup-preserving refactor: every item preserves rendered output. Run commands from `apps/web-ui/gateway` unless stated otherwise. Generated `*_templ.go` are gitignored — run `templ generate`, never stage the output.

## 1 — Bump go-daisy pin

- [x] 1.1 `go get github.com/emergent-company/go-daisy@d0849e4` + `go mod tidy` + `GOWORK=off go mod vendor`; confirm `go.mod` pins `d0849e42b8a9` and `vendor/.../components/ui/` contains `dialog.templ` and `confirm-dialog.templ`. Verify: `go build ./...` green.

## 2 — Dialog

- [x] 2.1 Replace all 16 `modalShell` call sites (`ui.templ` agent-form + confirmDeleteDialog internals, `org_members_ui.templ`, `blueprints.templ`, `objects.templ`, `mcp_shares.templ` ×2, `agent_mcp_endpoint.templ` ×3, `org_context.templ`, `mcp_nodes.templ`, `sidepanel.templ`, `schema.templ` ×2, `project_settings.templ`, `auth_ui.templ`) with `ui.Dialog`. Delete `modalShell`; keep `modalBoxAttrs` (`checkboxPicker` reuses it). Verify: `go test ./...` green.

## 3 — ConfirmDialog

- [x] 3.1 Replace all 6 `confirmDeleteDialog` call sites (`ui.templ` deleteConfirmDialog, `backups.templ`, `schedules.templ`, `org_context.templ`, `mcp_servers.templ`, `skills.templ`) with `ui.ConfirmDialog`. Delete `confirmDeleteDialog`; keep `deleteConfirmDialog`/`deleteAgentDescription` (agent-specific wrappers). Verify: `go test ./...` green.

## 4 — Toast queue

- [x] 4.1 Replace the `@toastQueue()` call site with `ui.ToastQueueWithProps(ui.ToastQueueProps{Position: ui.ToastQueueBottomCenter, PauseOnHover: true, Countdown: true})`; delete `toast.templ` and `toast.go`. Verify: `#toast-container` id and `flashToast` contract preserved; `go test ./...` green.
- [x] 4.2 Remove the duplicate `.toast-bar` + `@keyframes toast-shrink` (and the reduced-motion `.toast-bar` line) from `webui/css/app.css`; confirm go-daisy `custom.css` is `@import`ed and blank-imported via `godaisy_css.go`. Verify: `task css` output still contains `.toast-bar` + `toast-shrink` (single source).

## 5 — Section Rows for cardList

- [x] 5.1 `cardList` titled branch → `ui.Section(..., Rows: true)`; title-empty branch stays `ui.CardRaw` (no `<section>` wrapper). `cardList` remains a thin alias so all 23 call sites (incl. `agent.templ`, later-lane-owned) compile unchanged. Verify: `go test ./...` green.

## 6 — CommandPaletteButton + FullScreenMobile

- [x] 6.1 Replace `spotlightTrigger` with `ui.CommandPaletteButton("spotlight", "Search…")` at the `ui.templ` Navbar call site; delete `spotlightTrigger`. Verify: `go test ./...` green.
- [x] 6.2 `spotlightSearch` sets `FullScreenMobile: true` and deletes the hand-rolled mobile `<style>` block. Verify: selectors derived from `ID: "spotlight"`; `go test ./...` green.

## 7 — OpenSpec

- [x] 7.1 Create `openspec/changes/adopt-go-daisy-library-components` (proposal, tasks, delta spec on `web-ui-components`). Verify: `openspec validate adopt-go-daisy-library-components` clean.

## 8 — Verification

- [x] 8.1 `templ generate ./...` && `go build ./...` && `go test ./...` && `task lint` (or golangci-lint/go vet/templ -check directly if lefthook is unavailable) — all clean.

## 9 — Second lane: bump pin again

- [x] 9.1 `go get github.com/emergent-company/go-daisy@8ce69ca` + `go mod tidy` + `GOWORK=off go mod vendor`; confirm `go.mod` pins `8ce69ca4cdd1`. Verify: `go build ./...` green.

## 10 — Detail/page headers onto nav.PageHeading

- [x] 10.1 `detailHeader` → thin adapter over `nav.PageHeading` (Margin/Dashboard/Bare/SubtitleFull/TitleAdornment + children actions slot). `Leading` variant stays local (`detailHeaderLeading`). `pageHeader` stays local. Verify: `go test ./...` green, `refactor_exact_test.go` unchanged.

## 11 — Sidebar row onto layout.SidebarNavItem

- [x] 11.1 Delete `sidebarNavRow`/`sidebarIndicatorID`; call `layout.SidebarNavItem` and pass the provider warning via `SidebarItem.Warn` (`sidebarProviderWarning`). Verify: `go test ./...` green.

## 12 — Account avatar onto AvatarFull TextClass

- [x] 12.1 `accountAvatar` passes the initials font-size via `AvatarProps.TextClass`, keeping the `shrink-0` wrapper. Verify: `go test ./...` green.

## 13 — Type accents onto IconTile/Badge Color+Glyph

- [x] 13.1 `typeIconTile`/`typeNameChip` resolve the catalog then delegate to `ui.IconTile`/`ui.Badge` `Color`/`Glyph`/`LabelClass`; keep `typeIconClass`/`normalizeIconName` local. Verify: `go test ./...` green.

## 14 — Agent tool disclosure (stays local)

- [x] 14.1 Keep `agentToolDisclosure` local: `ui.Disclosure` has no summary-attributes slot, hardcodes `bg-base-200/40` on the details, and hardcodes `gap-2 border-base-content/10 p-3` on the body — none match the source-group variant. Verify: `agent_ui_test.go` unchanged and green.

## 15 — cardList onto Section.Wrapperless

- [x] 15.1 Collapse `cardList` onto `ui.Section` using `Wrapperless: title == ""` and `Rows: true`. Verify: `go test ./...` green.

## 16 — OpenSpec (extend existing change)

- [x] 16.1 Extend `adopt-go-daisy-library-components` (proposal, tasks, delta spec) with this lane. Verify: `openspec validate adopt-go-daisy-library-components` clean.

## 17 — Verification (second lane)

- [x] 17.1 `task css` (from gateway) && `templ generate ./...` && `go build ./...` && `go test ./...` && `task lint` — all clean.
