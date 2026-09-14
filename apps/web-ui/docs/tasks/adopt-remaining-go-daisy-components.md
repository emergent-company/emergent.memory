# Adopt remaining go-daisy components skipped for API mismatch

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-godaisy-component-backport](../sessions/2026-09-10-godaisy-component-backport.md)

## What
Revisit the upstream adoptions deliberately skipped in PR #22/#24 because go-daisy's API could not preserve current behavior. Either migrate the app to the upstream pattern or extend upstream to cover the app's needs:

- `components/modal` (`Modal`/`FormModal`/`ConfirmPopup`/`DeleteButton`) — app uses native `<dialog class="modal">` + `showModal()` + app.js (`openDeleteConfirm`/`openAgentForm`); upstream uses always-open `modal-open` + htmx POST→`#modal-container`. Reconcile (extend upstream to support the JS/dialog driver, or migrate the app flow).
- `layout.Sidebar` — app's `appSidebar`/`sidebarNavRow` is a documented element-for-element copy (adds a `warn` indicator, own toggle wiring, `memoryLogo` header). Extend upstream Sidebar to cover those hooks, then delete the local copy.
- `form.StructuredInput` — app's `mcpServerKVEditor`/`mcpServerKVRow` is app.js-driven with flat `name="env.key"` serialization; upstream is Alpine `x-model` + array hidden inputs. Reconcile mechanism/wire format.
- Checkbox/toggle pickers — `apiTokenScopePickerContent` (grouped grid + card labels) and the toggle-based agent/mcp tool rows were not unified because their shape/input type differs.

## Why
Removing these duplicates shrinks the gateway UI surface and keeps behavior in one place — but only once upstream can preserve the exact UX. Forcing adoption earlier would have regressed pixels or the form wire format, so it was deferred rather than half-done.

## Depends on
- none

## Notes
- Upstream: `github.com/emergent-company/go-daisy`; release a real minor tag after changes and bump the gateway pin + `go mod vendor`.
- App files: `gateway/ui.templ` (`modalShell`/`confirmDeleteDialog`), `gateway/sidebar_user.templ`, `gateway/mcp_servers.templ`, `gateway/api_tokens.templ`, `gateway/agent.templ`.
- Precedent: `modal.*` already exists in the pinned go-daisy commit but is unvendored (unused), so adopting it is a `go mod vendor` away once the flow is compatible.
