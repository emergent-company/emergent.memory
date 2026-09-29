## 1. Drawer shell

- [x] 1.1 Add `objectPreviewDrawer()` templ (backdrop, right-hand `role="dialog"` aside, static header with preview icon + edit link + close control, `#object-preview-body` HTMX swap target) and mount it in appShell beside `sidePanel` so it survives HTMX navigation
- [x] 1.2 Add `objectPreviewContent(...)` partial rendering the object icon/type chip, display label, properties as a `<dl>` of label/value rows, details (type/key/status/created/id), and labels; include a placeholder actions slot for future comments/actions
- [x] 1.3 Render empty property values in a muted style with an empty marker so a future hide/fold toggle can target them

## 2. Server partial

- [x] 2.1 Add `uiObjectPreviewPartial` handler reusing the existing object + compiled-types (+ optional relationship) fetch seam; render `objectPreviewContent`, or `objectPreviewNotFound` on fetch failure
- [x] 2.2 Register `GET /objects/:id/preview` in `main.go`
- [x] 2.3 Unit-test the partial: read-only (no form controls), heading id, empty-value marker, drawer a11y ids, value formatting, and route/script wiring

## 3. Client behaviour

- [x] 3.1 Add `object-preview.js`: open/close, backdrop + Escape + close button, light focus handling, HTMX body load with loading and error fallback
- [x] 3.2 Intercept only plain left-clicks on `a[href^="/objects/"]` inside chat markdown or sources-list entries; preserve `href`; let modified clicks, non-primary buttons, `target`/`download`, and multi-segment refs fall through to native navigation
- [x] 3.3 Point the header edit action at the clicked reference's `/objects/<id>` path on open

## 4. Build and verify

- [x] 4.1 `task generate` (templ) then `go build ./...` from `gateway/`; `go test ./...`
- [x] 4.2 `task lint` (gateway + webui lefthook group)
- [ ] 4.3 Restart the dev server and manually verify in the browser: drawer slide-in/backdrop/Escape/focus, htmx swap + loading, edit-link href, sources-block links, not-found state

## 5. Spec

- [x] 5.1 Add capability spec `web-object-reference-preview` and update the legacy design note `docs/spec/36-chat-object-references.md`
