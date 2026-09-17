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
