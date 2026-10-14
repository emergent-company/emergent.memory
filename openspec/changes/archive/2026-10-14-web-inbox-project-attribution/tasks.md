## 1. Account-inbox row attribution

- [x] 1.1 `inboxView` gains `ProjectNames map[string]string`; `uiInbox` builds the id→name map from `s.memory.ListProjects(ctx)` (best-effort — a failure leaves an empty map and rows still render).
- [x] 1.2 `notificationProjectLabel(d inboxView, n Notification) string` returns the project name only for account scope + non-empty `ProjectID`, `""` otherwise. Unit-tested.
- [x] 1.3 `inbox.templ` threads `d` through `inboxList` → `inboxRow` → `inboxRowBody` and renders a neutral XS folder badge next to the title when the label is non-empty. Verify: `go build ./...`, `go test ./...`.

## 2. Account project filter

- [x] 2.1 `inboxView` gains `ProjectFilter string` + `ProjectOptions []ProjectRef`; `uiInbox` reads `?project=` and validates it against the fetched project list (unknown ids ignored — fail closed).
- [x] 2.2 `inboxProjectFilter` passes the explicit account filter through (project scope stays the active project). Doc comment updated to distinguish the explicit filter from the #1342 auto-narrow.
- [x] 2.3 `inboxFilterHref(scope, tab, project)` builds links preserving scope+tab+project; the tab strip uses it.
- [x] 2.4 `inbox.templ` renders the account-scope project filter (hidden `scope`/`tab` + a native `select` that submits on change), only when `d.Scope == "account"`. Verify: `go test ./...`.
- [x] 2.5 `inbox.js` `refreshList()` preserves `scope`, `tab`, and `project`. Verify: `node --check webui/static/js/inbox.js`.

## 3. Sidebar + topbar

- [x] 3.1 `ui.go` removes the Inbox entry from `sidebarGroups`.
- [x] 3.2 `notifications_bell.templ` swaps `lucide--bell` for `lucide--inbox` and updates the doc comment; anchors/testid unchanged. Verify: `go test ./...`.

## 4. Tests

- [x] 4.1 `inbox_ui_test.go`: sidebar assertions updated for the Inbox removal; add assertions that the chip + filter render for account scope and not for project scope; keep bell-anchor tests passing.
- [x] 4.2 `inbox_view_test.go`: unit-test `notificationProjectLabel`.

## 5. Verification

- [x] 5.1 `templ generate`, `go build ./...`, `go test ./...`, and `task lint` in `apps/web-ui/gateway`.
