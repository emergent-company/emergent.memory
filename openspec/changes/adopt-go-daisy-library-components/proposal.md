## Why

`extract-shared-ui-components` and `refactor-webui-component-adoption` moved the gateway's twelve highest-leverage patterns into a local `components/` package. But five of the patterns those changes deliberately deferred — the ones that duplicate a go-daisy component rather than a sibling template — are now upstream: the gateway's `modalShell`, `confirmDeleteDialog`, `toastQueue`, `cardList`, and `spotlightTrigger` were ported into `go-daisy` (PRs #11, #12, #13) as `ui.Dialog`, `ui.ConfirmDialog`, `ui.ToastQueueWithProps`, `ui.Section`'s `Rows`/`Titleless` options, and `ui.CommandPaletteButton` + `ui.CommandPalette`'s `FullScreenMobile`.

Every one of these is now carried twice: once in `go-daisy` (the source of truth, compiler-checked and gallery-visible) and once in the gateway (`ui.templ`, `toast.templ`/`toast.go`, `spotlight.templ`). The gateway copies drift independently — the local `toastQueueInit` even still carries a 4200ms default duration while upstream uses 4000ms, and the local spotlight mobile CSS duplicates `commandPaletteMobileCSS`. This change adopts the upstream components and deletes the gateway copies.

## What Changes

Bump the go-daisy pin to `d0849e4` (contains the upstreamed components) and repoint each gateway call site at the library component, preserving rendered output. All five migrations are markup-preserving:

1. `modalShell` (16 call sites) → `ui.Dialog(ui.DialogProps{ID, BoxID, BoxClass, Attrs})`. The local `modalShell`/`modalBoxAttrs` are deleted (`modalBoxAttrs` is kept — `checkboxPicker` reuses it for an unrelated wrapper id).
2. `confirmDeleteDialog` (6 call sites) → `ui.ConfirmDialog(ui.ConfirmDialogProps{ID, Noun, Title, Icon, BoxClass, Attrs}, description)`. The local `confirmDeleteDialog` is deleted; `deleteConfirmDialog`/`deleteAgentDescription` (agent-specific wrappers) remain.
3. `toastQueue` (1 call site) → `ui.ToastQueueWithProps(ui.ToastQueueProps{Position: ui.ToastQueueBottomCenter, PauseOnHover: true, Countdown: true})`. The `#toast-container` id contract and the `flashToast`/`app.js` `queue.add({type,message,duration:4200})` contract are preserved. `toast.templ` and `toast.go` are deleted. The gateway's duplicate `.toast-bar` + `@keyframes toast-shrink` CSS is removed from `webui/css/app.css` — `go-daisy`'s `components/css/custom.css` (already `@import`ed and blank-imported via `godaisy_css.go`) is now the single source.
4. `cardList` titled branch (23 call sites) → `ui.Section(..., Rows: true)`; the title-empty branch stays a `ui.CardRaw` because `ui.Section` always emits a `<section>` wrapper the bare variant never had. `cardList` is kept as a thin alias so the 23 call sites (including the two in `agent.templ`, owned by a later lane) compile unchanged.
5. `spotlightTrigger` (1 call site) → `ui.CommandPaletteButton("spotlight", "Search…")`; `spotlightSearch` sets `FullScreenMobile: true` and the hand-rolled mobile `<style>` block is deleted (the library emits the identical selectors from `props.ID`).

## Capabilities

### Modified Capabilities

- `web-ui-components`: extends the shared-component capability to cover the gateway adopting the upstreamed go-daisy components (`ui.Dialog`, `ui.ConfirmDialog`, `ui.ToastQueueWithProps`, `ui.Section` `Rows`, `ui.CommandPaletteButton`/`FullScreenMobile`) and deleting its local copies, with the no-regression requirement that rendered output is unchanged.

## Impact

- `apps/web-ui/gateway/go.mod`, `go.sum` — go-daisy pin `d98f93ca60f9` → `d0849e42b8a9`.
- `apps/web-ui/gateway/ui.templ` — delete `modalShell` and `confirmDeleteDialog`; `cardList` adopts `Section` `Rows`; toast + spotlight call sites repoint.
- `apps/web-ui/gateway/toast.templ`, `toast.go` — deleted.
- `apps/web-ui/gateway/spotlight.templ` — delete `spotlightTrigger` + mobile `<style>`; `FullScreenMobile: true`.
- `apps/web-ui/gateway/{backups,schedules,mcp_servers,skills,org_context,org_members_ui,objects,mcp_shares,mcp_nodes,agent_mcp_endpoint,project_settings,blueprints,schema,sidepanel,auth_ui}.templ` — call-site swaps to `ui.Dialog` / `ui.ConfirmDialog`.
- `apps/web-ui/gateway/webui/css/app.css` — delete the duplicate `.toast-bar` + `@keyframes toast-shrink` (now sourced from go-daisy `custom.css`).
- `apps/web-ui/gateway/refactor_exact_test.go` — repoint the `modalShell`/`confirmDeleteDialog` exact-render assertions at `ui.Dialog`/`ui.ConfirmDialog` (the `want` strings are unchanged: output is preserved).
- Net effect: ~180 gateway lines deleted; no route, handler, API, schema, or user-visible behavior change.
