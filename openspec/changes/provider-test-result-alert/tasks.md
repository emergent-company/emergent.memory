# Tasks

Implementation plan for `provider-test-result-alert`. All work is in `apps/web-ui`.

## 1. Alert dialog component

- [x] 1.1 Add `components.AlertModal` (native `<dialog>` shell) with banner/title/message ids, an `aria-labelledby` pointing at the live title, and a Close button (`method="dialog"` backdrop + Escape handled natively)
- [x] 1.2 Component contract test: ids, `role="alert"`, accessible name, no auto-open

## 2. Client wiring

- [x] 2.1 Add `MemoryApp.alert(kind, title, message)` — maps kind to banner tone/icon, writes title/message with `textContent`, opens with `showModal()` guarded on absence
- [x] 2.2 Delegate a `memory-alert` body event to `alertModal`; render the modal once in the app shell
- [x] 2.3 js-dom spec: open, event wiring, tone mapping, XSS boundary (text not markup), Close/Escape, no-op when absent

## 3. Server triggers + save gating

- [x] 3.1 Add `alertTrigger` (HX-Trigger `memory-alert` with `{kind,title,message}`, HTTP 200)
- [x] 3.2 Migrate provider test, connection test, and default-model test handlers from `memory-toast`
- [x] 3.3 Update provider handler tests to assert `memory-alert` + title/message
- [x] 3.4 Re-point the connection-test Save gating in `project_settings.templ` to `memory-alert`
