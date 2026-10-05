## Why

Provider and model test outcomes (Settings → Providers) were surfaced as transient
toasts. A provider call can return a long reply snippet or a multi-line upstream
error, which a toast auto-dismisses before it can be read, and toasts are not
focus-managed or dismissible on demand. The three test actions (provider test,
connection test, default-model test) need a persistent, keyboard-accessible
result surface.

## What Changes

- Add an app-wide alert dialog to the shell (`components.AlertModal`, rendered in
  `appShell`) opened by a delegated `memory-alert` client event carrying
  `{kind, title, message}`.
- Migrate the provider/model test handlers from the `memory-toast` HX-Trigger to
  a new `memory-alert` HX-Trigger (`alertTrigger`), so outcomes open the dialog.
- Keep the connection-test save gating wired to the same event (a successful
  `memory-alert` enables Save).
- Render the dialog title/message with `textContent` (provider replies are
  untrusted — never `innerHTML`) and label the dialog by its live title.

## Capabilities

### Modified Capabilities

- `provider-settings`: test outcomes are presented in an accessible alert dialog
  rather than a transient toast; the connection-test success that enables Save is
  the same dialog event.

## Impact

- `apps/web-ui/gateway/components/alert_modal.templ` — the dialog component.
- `apps/web-ui/gateway/ui.templ` — rendered once in the app shell.
- `apps/web-ui/gateway/webui/static/js/app.js` — `MemoryApp.alert` + the
  `memory-alert` listener.
- `apps/web-ui/gateway/settings_handlers.go` — `alertTrigger`.
- `apps/web-ui/gateway/settings_providers.go`, `project_settings.templ` — migrated
  triggers and save gating.
- Tests: component contract test, handler assertions, and a hermetic js-dom spec
  covering open/close, tone mapping, event wiring, and the XSS boundary.

## Non-Goals

- No change to the provider test APIs, response bodies, or persistence.
- No change to toast behaviour for other (CRUD) flows.
